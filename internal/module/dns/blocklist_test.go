package dns

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParseFeed_逐行去重与注释(t *testing.T) {
	body := "# comment\n! ignore\n\nexample.com\n*.bad.com\n\nexample.com\nEVIL.NET\n"
	entries := ParseFeed(body, "bl1")
	if len(entries) != 3 {
		t.Fatalf("want 3 unique entries, got %d: %+v", len(entries), entries)
	}
	if entries[2].Pattern != "evil.net" && entries[2].Pattern != "EVIL.NET" {
		t.Fatalf("pattern case preserved as-is: %s", entries[2].Pattern)
	}
}

func TestBuildRPZZone_动作映射与去重(t *testing.T) {
	entries := []Entry{
		{TriggerType: "qname", Pattern: "badsite.com", Action: "nxdomain"},
		{TriggerType: "qname", Pattern: "*.cdn.bad.com", Action: "nxdomain"},
		{TriggerType: "qname", Pattern: "gamble.com", Action: "redirect", RedirectTarget: "block.corp.local."},
		{TriggerType: "qname", Pattern: "badsite.com", Action: "nxdomain"},      // 重复
		{TriggerType: "response_ip", Pattern: "1.2.3.0/24", Action: "nxdomain"}, // 跳过
	}
	text, n := BuildRPZZone("students.rpz", entries)
	if n != 3 {
		t.Fatalf("want 3 deduped, got %d:\n%s", n, text)
	}
	if !strings.Contains(text, "badsite.com CNAME .") {
		t.Fatalf("nxdomain mapping wrong:\n%s", text)
	}
	if !strings.Contains(text, "gamble.com CNAME block.corp.local.") {
		t.Fatalf("redirect mapping wrong:\n%s", text)
	}
}

func TestSyncFeed_失败保留旧版(t *testing.T) {
	repo := NewMemBlocklistRepo()
	failing := func(context.Context, string) (string, error) { return "", errors.New("down") }
	svc := NewBlocklistService(repo, failing, &fakeCtl{}, "/tmp/rpz")

	bl, _ := repo.Create(context.Background(), Blocklist{Name: "feed", Kind: "feed", SyncURL: "http://x/f.txt"})
	if _, _, err := svc.SyncFeed(context.Background(), bl.ID); err == nil {
		t.Fatal("want FEED_DOWN")
	}
	// 旧版保留：version 仍为 1，无条目
	got, _, _ := repo.Get(context.Background(), bl.ID)
	if got.Version != 1 {
		t.Fatalf("version bumped on failure: %d", got.Version)
	}
}

func TestSyncFeed_成功去重与版本递增(t *testing.T) {
	repo := NewMemBlocklistRepo()
	feed := func(context.Context, string) (string, error) {
		return "a.com\n*.b.com\n", nil
	}
	svc := NewBlocklistService(repo, feed, &fakeCtl{}, "/tmp/rpz")
	bl, _ := repo.Create(context.Background(), Blocklist{Name: "f", Kind: "feed", SyncURL: "http://x"})

	added, total, err := svc.SyncFeed(context.Background(), bl.ID)
	if err != nil || added != 2 || total != 2 {
		t.Fatalf("added=%d total=%d err=%v", added, total, err)
	}
	// 再次同步同内容 → 去重，added=0
	added2, _, _ := svc.SyncFeed(context.Background(), bl.ID)
	if added2 != 0 {
		t.Fatalf("dedupe failed: added=%d", added2)
	}
	got, _, _ := repo.Get(context.Background(), bl.ID)
	if got.Version != 3 {
		t.Fatalf("version = %d want 3", got.Version)
	}
}

func TestCompile_增量编译返回zone信息(t *testing.T) {
	repo := NewMemBlocklistRepo()
	fc := &fakeCtl{}
	svc := NewBlocklistService(repo, nil, fc, "/tmp/rpz")
	bl, _ := repo.Create(context.Background(), Blocklist{Name: "学生名单", Kind: "custom"})
	_, _ = repo.UpsertEntries(context.Background(), []Entry{
		{ListID: bl.ID, TriggerType: "qname", Pattern: "bad.edu", Action: "nxdomain"},
	})
	g, _ := repo.CreatePolicyGroup(context.Background(), PolicyGroup{
		Name: "学生", ViewName: "students", Cidrs: []string{"10.61.128.0/17"}, ListIDs: []string{bl.ID},
	})
	zone, n, path, cmd, err := svc.Compile(context.Background(), g.ID)
	if err != nil || zone != "students.rpz" || n != 1 || path != "local-zone (runtime)" {
		t.Fatalf("zone=%s n=%d path=%s err=%v", zone, n, path, err)
	}
	if !strings.Contains(cmd, "local_zone bad.edu static") {
		t.Fatalf("local_zone cmd: %s", cmd)
	}
	if fc.localZones == 0 {
		t.Fatal("local_zone not triggered")
	}
}

func TestMemRepo_DeleteList_级联删条目(t *testing.T) {
	r := NewMemBlocklistRepo()
	bl, _ := r.Create(context.Background(), Blocklist{Name: "l1", Kind: "custom"})
	_, _ = r.UpsertEntries(context.Background(), []Entry{
		{ListID: bl.ID, TriggerType: "qname", Pattern: "*.a.com", Action: "nxdomain"},
		{ListID: bl.ID, TriggerType: "qname", Pattern: "*.b.com", Action: "nxdomain"},
	})
	if err := r.DeleteList(context.Background(), bl.ID); err != nil {
		t.Fatalf("DeleteList: %v", err)
	}
	ents, _ := r.ListEntries(context.Background(), bl.ID, "")
	if len(ents) != 0 {
		t.Fatalf("entries should be cascaded, got %d", len(ents))
	}
	if _, ok, _ := r.Get(context.Background(), bl.ID); ok {
		t.Fatal("list should be gone")
	}
}

func TestMemRepo_DeleteEntry_自然键与未找到(t *testing.T) {
	r := NewMemBlocklistRepo()
	bl, _ := r.Create(context.Background(), Blocklist{Name: "l2", Kind: "custom"})
	_, _ = r.UpsertEntries(context.Background(), []Entry{
		{ListID: bl.ID, TriggerType: "qname", Pattern: "*.a.com", Action: "nxdomain"},
		{ListID: bl.ID, TriggerType: "qname", Pattern: "*.b.com", Action: "nxdomain"},
	})
	if err := r.DeleteEntry(context.Background(), bl.ID, "*.a.com"); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}
	ents, _ := r.ListEntries(context.Background(), bl.ID, "")
	if len(ents) != 1 || ents[0].Pattern != "*.b.com" {
		t.Fatalf("want only *.b.com, got %+v", ents)
	}
	if err := r.DeleteEntry(context.Background(), bl.ID, "*.missing.com"); err != ErrBlocklistNotFound {
		t.Fatalf("want ErrBlocklistNotFound, got %v", err)
	}
}

func TestMatchPolicyView_CIDR首中与边界(t *testing.T) {
	groups := []PolicyGroup{
		{ViewName: "v10", Cidrs: []string{"10.0.0.0/8"}},
		{ViewName: "v192", Cidrs: []string{"192.168.1.0/24", "192.168.2.0/24"}},
	}
	if v, ok := MatchPolicyView(groups, "10.99.1.1"); !ok || v != "v10" {
		t.Fatalf("want v10, got %q %v", v, ok)
	}
	if v, ok := MatchPolicyView(groups, "192.168.2.9"); !ok || v != "v192" {
		t.Fatalf("want v192, got %q %v", v, ok)
	}
	if _, ok := MatchPolicyView(groups, "8.8.8.8"); ok {
		t.Fatal("no match should be false")
	}
	if _, ok := MatchPolicyView(groups, "not-an-ip"); ok {
		t.Fatal("bad ip should be false")
	}
	if _, ok := MatchPolicyView([]PolicyGroup{{ViewName: "v", Cidrs: []string{"bad-cidr"}}}, "10.0.0.1"); ok {
		t.Fatal("bad cidr should be skipped")
	}
}
