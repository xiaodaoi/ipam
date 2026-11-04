package dns

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestScheduleActive(t *testing.T) {
	// 2026-11-04 是周三（3）。
	wed10 := time.Date(2026, 11, 4, 10, 0, 0, 0, time.Local)
	wed23 := time.Date(2026, 11, 4, 23, 0, 0, 0, time.Local)
	thu3 := time.Date(2026, 11, 5, 3, 0, 0, 0, time.Local)

	cases := []struct {
		name string
		s    *Schedule
		at   time.Time
		want bool
	}{
		{"nil 全天生效", nil, wed10, true},
		{"空字段全天生效", &Schedule{}, wed23, true},
		{"工作日白天", &Schedule{Days: []int{1, 2, 3, 4, 5}, Start: "09:00", End: "18:00"}, wed10, true},
		{"工作日夜间", &Schedule{Days: []int{1, 2, 3, 4, 5}, Start: "09:00", End: "18:00"}, wed23, false},
		{"跨零点窗口内", &Schedule{Start: "22:00", End: "06:00"}, wed23, true},
		{"跨零点窗口内(次日)", &Schedule{Start: "22:00", End: "06:00"}, thu3, true},
		{"跨零点窗口外", &Schedule{Start: "22:00", End: "06:00"}, wed10, false},
		{"星期不匹配", &Schedule{Days: []int{0, 6}, Start: "00:00", End: "23:59"}, wed10, false},
		{"非法时间视为不限", &Schedule{Start: "25:99", End: "x"}, wed10, true},
	}
	for _, tc := range cases {
		if got := tc.s.Active(tc.at); got != tc.want {
			t.Fatalf("%s: got=%v want=%v", tc.name, got, tc.want)
		}
	}
}

func TestCompile_ScheduleInactiveSkipsApply(t *testing.T) {
	repo := NewMemBlocklistRepo()
	fc := &fakeCtl{}
	svc := NewBlocklistService(repo, nil, fc, "/tmp/rpz")
	bl, _ := repo.Create(context.Background(), Blocklist{Name: "l", Kind: "custom"})
	_, _ = repo.UpsertEntries(context.Background(), []Entry{
		{ListID: bl.ID, TriggerType: "qname", Pattern: "bad.example", Action: "nxdomain"},
	})
	g, _ := repo.CreatePolicyGroup(context.Background(), PolicyGroup{
		Name: "g", ViewName: "v", Cidrs: []string{"10.0.0.0/8"}, ListIDs: []string{bl.ID},
		Schedule: &Schedule{Days: []int{0}, Start: "00:00", End: "01:00"},
	})
	svc.now = func() time.Time { return time.Date(2026, 11, 4, 12, 0, 0, 0, time.Local) } // 周三
	zone, n, path, _, err := svc.Compile(context.Background(), g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if zone != "v.rpz" || n != 0 || path != "schedule inactive" {
		t.Fatalf("zone=%s n=%d path=%s", zone, n, path)
	}
	if fc.localZones != 0 {
		t.Fatalf("窗口外不应下发，实际 %d", fc.localZones)
	}
}

func TestReconcile_RemovesZonesOutsideWindow(t *testing.T) {
	repo := NewMemBlocklistRepo()
	fc := &fakeCtl{}
	svc := NewBlocklistService(repo, nil, fc, "/tmp/rpz")
	bl, _ := repo.Create(context.Background(), Blocklist{Name: "l", Kind: "custom"})
	_, _ = repo.UpsertEntries(context.Background(), []Entry{
		{ListID: bl.ID, TriggerType: "qname", Pattern: "bad.example", Action: "nxdomain"},
	})
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.Local)
	svc.now = func() time.Time { return now }
	_, _ = repo.CreatePolicyGroup(context.Background(), PolicyGroup{
		Name: "g", ViewName: "v", Cidrs: []string{"10.0.0.0/8"}, ListIDs: []string{bl.ID},
		Schedule: &Schedule{Start: "09:00", End: "11:00"},
	})

	if n, err := svc.Reconcile(context.Background()); err != nil || n != 1 {
		t.Fatalf("窗口内 n=%d err=%v", n, err)
	}
	// 进入窗口外：再次收敛应撤销。
	now = now.Add(3 * time.Hour)
	if n, err := svc.Reconcile(context.Background()); err != nil || n != 0 {
		t.Fatalf("窗口外 n=%d err=%v", n, err)
	}
	if len(fc.removedZones) != 1 || fc.removedZones[0] != "bad.example" {
		t.Fatalf("removedZones=%v", fc.removedZones)
	}
}

func TestReconcile_RedirectUsesBlockPageIP(t *testing.T) {
	repo := NewMemBlocklistRepo()
	fc := &fakeCtl{}
	svc := NewBlocklistService(repo, nil, fc, "/tmp/rpz")
	svc.SetBlockPageResolver(func(context.Context) string { return "10.61.40.9" })
	bl, _ := repo.Create(context.Background(), Blocklist{Name: "l", Kind: "custom"})
	_, _ = repo.UpsertEntries(context.Background(), []Entry{
		{ListID: bl.ID, TriggerType: "qname", Pattern: "*.gamble.example", Action: "redirect"},
	})
	_, _ = repo.CreatePolicyGroup(context.Background(), PolicyGroup{
		Name: "g", ViewName: "v", Cidrs: []string{"10.0.0.0/8"}, ListIDs: []string{bl.ID},
	})
	if _, err := svc.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	// unbound local-data 不展开 *.name：通配 redirect 必须用 redirect 区，子域自动命中。
	if fc.zoneTypes["gamble.example"] != "redirect" {
		t.Fatalf("zoneType=%q", fc.zoneTypes["gamble.example"])
	}
	joined := strings.Join(fc.localData, "\n")
	if !strings.Contains(joined, "gamble.example 300 IN A 10.61.40.9") ||
		strings.Contains(joined, "*.gamble.example") {
		t.Fatalf("localData=%v", fc.localData)
	}
}

func TestParseEntryCSV(t *testing.T) {
	content := "\uFEFFpattern,action,category\n" +
		"*.doubleclick.net,nxdomain,ads\n" +
		"bad.example.com,redirect,gambling\n" +
		"# comment row\n" +
		"\n" +
		"bad.example.com,nxdomain,dup\n" +
		"in*valid.example,nxdomain,bad\n" +
		"plain.example,blocked,unknown\n"
	entries, skipped := ParseEntryCSV(content)
	if len(entries) != 2 {
		t.Fatalf("entries=%d %+v", len(entries), entries)
	}
	if entries[0].Pattern != "*.doubleclick.net" || entries[0].Action != "nxdomain" || entries[0].Category != "ads" {
		t.Fatalf("first=%+v", entries[0])
	}
	if entries[1].Action != "redirect" {
		t.Fatalf("second=%+v", entries[1])
	}
	if len(skipped) != 3 {
		t.Fatalf("skipped=%v", skipped)
	}

	out := string(EntriesCSV(entries))
	if !strings.HasPrefix(out, "\uFEFFpattern,action,category\n") || !strings.Contains(out, "*.doubleclick.net,nxdomain,ads") {
		t.Fatalf("csv out=%q", out)
	}
}

func TestImportCSV_SnapshotAndRollback(t *testing.T) {
	repo := NewMemBlocklistRepo()
	svc := NewBlocklistService(repo, nil, &fakeCtl{}, "/tmp/rpz")
	bl, _ := repo.Create(context.Background(), Blocklist{Name: "l", Kind: "custom"})
	_, _ = repo.UpsertEntries(context.Background(), []Entry{
		{ListID: bl.ID, TriggerType: "qname", Pattern: "old.example", Action: "nxdomain"},
	})

	created, skipped, total, err := svc.ImportCSV(context.Background(), bl.ID, "new1.example\nnew2.example\n", false)
	if err != nil || created != 2 || len(skipped) != 0 || total != 3 {
		t.Fatalf("created=%d skipped=%v total=%d err=%v", created, skipped, total, err)
	}
	versions, _ := repo.VersionList(context.Background(), bl.ID)
	if len(versions) != 1 || versions[0].Version != 1 || versions[0].Entries != 1 {
		t.Fatalf("versions=%+v", versions)
	}

	got, err := svc.Rollback(context.Background(), bl.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 3 {
		t.Fatalf("回滚后版本应递增: %d", got.Version)
	}
	ents, _ := repo.ListEntries(context.Background(), bl.ID, "")
	if len(ents) != 1 || ents[0].Pattern != "old.example" {
		t.Fatalf("回滚条目=%+v", ents)
	}
}

func TestInstallBuiltinPack(t *testing.T) {
	repo := NewMemBlocklistRepo()
	svc := NewBlocklistService(repo, nil, &fakeCtl{}, "/tmp/rpz")
	b, err := svc.InstallBuiltinPack(context.Background(), "ads")
	if err != nil {
		t.Fatal(err)
	}
	if b.Kind != "builtin" || b.Name == "" {
		t.Fatalf("pack=%+v", b)
	}
	ents, _ := repo.ListEntries(context.Background(), b.ID, "")
	if len(ents) == 0 {
		t.Fatal("内置包未写入条目")
	}
	if _, err := svc.InstallBuiltinPack(context.Background(), "ads"); err != ErrListNameDup {
		t.Fatalf("重复安装应冲突，err=%v", err)
	}
	if _, err := svc.InstallBuiltinPack(context.Background(), "unknown"); err != ErrPackNotFound {
		t.Fatalf("未知包 err=%v", err)
	}
}

func TestValidateRecordName(t *testing.T) {
	for _, ok := range []string{"", "*", "www", "*.example.com", "a.b.example.com", "*.corp.local."} {
		if err := ValidateRecordName(ok); err != nil {
			t.Fatalf("%q 应通过: %v", ok, err)
		}
	}
	for _, bad := range []string{"a.*.example.com", "**.example.com", "a..b", "*.", "x*y.example.com"} {
		if err := ValidateRecordName(bad); err == nil {
			t.Fatalf("%q 应拒绝", bad)
		}
	}
}

func TestReconcile_ZoneTypeSwitch(t *testing.T) {
	repo := NewMemBlocklistRepo()
	fc := &fakeCtl{}
	svc := NewBlocklistService(repo, nil, fc, "/tmp/rpz")
	svc.SetBlockPageResolver(func(context.Context) string { return "10.61.40.9" })
	bl, _ := repo.Create(context.Background(), Blocklist{Name: "l", Kind: "custom"})
	_, _ = repo.UpsertEntries(context.Background(), []Entry{
		{ListID: bl.ID, TriggerType: "qname", Pattern: "*.switch.example", Action: "redirect"},
	})
	_, _ = repo.CreatePolicyGroup(context.Background(), PolicyGroup{
		Name: "g", ViewName: "v", Cidrs: []string{"10.0.0.0/8"}, ListIDs: []string{bl.ID},
	})
	if _, err := svc.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fc.zoneTypes["switch.example"] != "redirect" {
		t.Fatalf("首轮应为 redirect: %v", fc.zoneTypes)
	}

	// 条目改为 NXDOMAIN：同一区必须从 redirect 切成 static（先删后加）。
	_, _ = repo.ClearEntries(context.Background(), bl.ID)
	_, _ = repo.UpsertEntries(context.Background(), []Entry{
		{ListID: bl.ID, TriggerType: "qname", Pattern: "switch.example", Action: "nxdomain"},
	})
	if _, err := svc.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fc.zoneTypes["switch.example"] != "static" {
		t.Fatalf("切换后应为 static: %v", fc.zoneTypes)
	}
	if len(fc.removedZones) == 0 {
		t.Fatal("类型切换应先移除旧区")
	}
}
