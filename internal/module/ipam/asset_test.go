package ipam

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAssetUpsert_MAC归一化与幂等(t *testing.T) {
	orgs := NewMemOrgStore()
	repo := NewMemAssetRepo()
	svc := NewAssetService(repo, orgs)

	saved, err := svc.Upsert(context.Background(), Asset{MAC: "AA-BB-CC-DD-EE-FF", Owner: "张三", Dept: "研发"})
	if err != nil {
		t.Fatal(err)
	}
	if saved.MAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("mac not normalized: %q", saved.MAC)
	}
	// 幂等更新（同 MAC 换备注）
	if _, err := svc.Upsert(context.Background(), Asset{MAC: "aa:bb:cc:dd:ee:ff", Owner: "张三", Note: "打印机"}); err != nil {
		t.Fatal(err)
	}
	list, _ := repo.List(context.Background(), AssetQuery{})
	if len(list) != 1 || list[0].Note != "打印机" {
		t.Fatalf("upsert not idempotent: %+v", list)
	}
}

func TestAssetUpsert_非法MAC拒绝(t *testing.T) {
	svc := NewAssetService(NewMemAssetRepo(), NewMemOrgStore())
	if _, err := svc.Upsert(context.Background(), Asset{MAC: "not-a-mac"}); err == nil {
		t.Fatal("want err on bad mac")
	}
}

func TestAssetUpsert_无标签规整为空数组(t *testing.T) {
	// 回归 M2-048：PG tags 列 NOT NULL，服务层须把 nil 规整为空数组，避免显式 NULL 覆盖默认值。
	repo := NewMemAssetRepo()
	svc := NewAssetService(repo, NewMemOrgStore())
	saved, err := svc.Upsert(context.Background(), Asset{MAC: "AA:BB:CC:DD:EE:09", Owner: "赵六"})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Tags == nil {
		t.Fatal("无标签时应规整为空数组，而非 nil")
	}
	if len(saved.Tags) != 0 {
		t.Fatalf("tags 应为空数组: %+v", saved.Tags)
	}
	// 显式标签原样保留
	saved2, _ := svc.Upsert(context.Background(), Asset{MAC: "aa:bb:cc:dd:ee:0a", Owner: "钱七", Tags: []string{"研发"}})
	if len(saved2.Tags) != 1 || saved2.Tags[0] != "研发" {
		t.Fatalf("显式标签被破坏: %+v", saved2.Tags)
	}
}

func TestAssetList_过滤(t *testing.T) {
	repo := NewMemAssetRepo()
	svc := NewAssetService(repo, NewMemOrgStore())
	_, _ = svc.Upsert(context.Background(), Asset{MAC: "aa:bb:cc:dd:ee:01", Owner: "张三"})
	_, _ = svc.Upsert(context.Background(), Asset{MAC: "aa:bb:cc:dd:ee:02", Owner: "李四", Dept: "财务"})

	if list, _ := repo.List(context.Background(), AssetQuery{Q: "李四"}); len(list) != 1 {
		t.Fatalf("q filter: %+v", list)
	}
	if list, _ := repo.List(context.Background(), AssetQuery{Q: "财务"}); len(list) != 1 {
		t.Fatalf("dept filter: %+v", list)
	}
	if err := svc.Delete(context.Background(), "AA:BB:CC:DD:EE:01"); err != nil {
		t.Fatal(err)
	}
	if list, _ := repo.List(context.Background(), AssetQuery{}); len(list) != 1 || strings.Contains(list[0].MAC, "01") {
		t.Fatalf("delete failed: %+v", list)
	}
}

func TestDiscoverUnregistered(t *testing.T) {
	repo := NewMemAssetRepo()
	ctx := context.Background()
	_ = repo.Upsert(ctx, Asset{MAC: "aa:bb:cc:dd:ee:01", Owner: "已登记"})
	svc := NewAssetService(repo, NewMemOrgStore())
	got, err := svc.Discover(ctx, []LedgerBinding{
		{MAC: "AA-BB-CC-DD-EE-01", IPv4: "10.0.0.1", State: "active"},
		{MAC: "aa:bb:cc:dd:ee:02", IPv4: "10.0.0.2", IPv6: "fd00::2", Hostname: "new-host", State: "active", Cltt: 1700000000},
		{MAC: "aa:bb:cc:dd:ee:02", IPv4: "10.0.0.2", State: "active"},
		{MAC: "aa:bb:cc:dd:ee:03", IPv4: "10.0.0.3", State: "inactive"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].MAC != "aa:bb:cc:dd:ee:02" || got[0].Hostname != "new-host" || got[0].IPv6 != "fd00::2" {
		t.Fatalf("got=%+v", got)
	}
}

func TestAssetUpsert_DUIDOnly(t *testing.T) {
	// M6-046：IPv6-only 终端只有 DUID，没有 MAC —— 身份键回退 DUID。
	repo := NewMemAssetRepo()
	svc := NewAssetService(repo, NewMemOrgStore())
	saved, err := svc.Upsert(context.Background(), Asset{
		DUID: "000100012DF9B6D9F8A2D6C97867", Owner: "李四"})
	if err != nil {
		t.Fatal(err)
	}
	if saved.MAC != "" || saved.DUID != "000100012df9b6d9f8a2d6c97867" {
		t.Fatalf("DUID 归一化失败: %+v", saved)
	}
	if saved.Identity() != saved.DUID {
		t.Fatalf("identity 应回退 DUID: %q", saved.Identity())
	}
	if a, ok, _ := svc.Get(context.Background(), saved.DUID); !ok || a.Owner != "李四" {
		t.Fatalf("按 DUID 读不到: %+v ok=%v", a, ok)
	}
}

func TestAssetIdentityValidation(t *testing.T) {
	svc := NewAssetService(NewMemAssetRepo(), NewMemOrgStore())
	if _, err := svc.Upsert(context.Background(), Asset{Owner: "x"}); !errors.Is(err, ErrBadIdentity) {
		t.Fatalf("缺身份键应报 BAD_IDENTITY: %v", err)
	}
	if _, err := svc.Upsert(context.Background(), Asset{MAC: "zz:zz", Owner: "x"}); !errors.Is(err, ErrBadMAC) {
		t.Fatalf("非法 MAC 应报 BAD_MAC: %v", err)
	}
	if _, err := svc.Upsert(context.Background(), Asset{DUID: "not-hex", Owner: "x"}); !errors.Is(err, ErrBadDUID) {
		t.Fatalf("非法 DUID 应报 BAD_DUID: %v", err)
	}
	if _, err := svc.Upsert(context.Background(), Asset{MAC: "aa:bb:cc:dd:ee:01"}); !errors.Is(err, ErrOwnerEmpty) {
		t.Fatalf("空使用人应报 OWNER_REQUIRED: %v", err)
	}
}

func TestAssetDelete_NotFoundAndSearchCompact(t *testing.T) {
	repo := NewMemAssetRepo()
	svc := NewAssetService(repo, NewMemOrgStore())
	if _, err := svc.Upsert(context.Background(), Asset{MAC: "68:8F:C9:5B:39:CF", Owner: "张三", Dept: "研发"}); err != nil {
		t.Fatal(err)
	}
	// 省略冒号/短横线都能命中（前端搜索框常见输入）。
	for _, q := range []string{"688fc95b39cf", "68-8f-c9", "68:8F:C9", "研发", "张三"} {
		if list, err := repo.List(context.Background(), AssetQuery{Q: q}); err != nil || len(list) != 1 {
			t.Fatalf("q=%q 应命中 1 条，实际 %d (%v)", q, len(list), err)
		}
	}
	if err := svc.Delete(context.Background(), "aa:bb:cc:dd:ee:99"); !errors.Is(err, ErrAssetNotFound) {
		t.Fatalf("删除不存在的资产应报 404: %v", err)
	}
	if err := svc.Delete(context.Background(), "688FC95B39CF"); err != nil {
		t.Fatalf("省略分隔符删除应成功: %v", err)
	}
	if list, _ := repo.List(context.Background(), AssetQuery{}); len(list) != 0 {
		t.Fatalf("删除后应为空: %+v", list)
	}
}

func TestDiscoverResolvesDUIDIdentity(t *testing.T) {
	// 三类 DUID：映射表命中 / LLT 推导 / UUID 无 MAC 回退。
	duidLLT := "000100012f51e6c570089431ccb5" // 0001 0001 + time + MAC 70089431ccb5
	duidUUID := "0004e8127e03bbbf68d30d09082e0c826c7c"
	duidMapped := "000100012df9b6d9aabbccddeeff" // 映射表命中 aabbccddeeff
	svc := NewAssetService(NewMemAssetRepo(), NewMemOrgStore())
	svc.SetIdentityMapping(func(context.Context) (map[string]string, error) {
		return map[string]string{duidMapped: "aa:bb:cc:dd:ee:ff"}, nil
	})
	now := time.Now().Unix()
	got, err := svc.Discover(context.Background(), []LedgerBinding{
		{MAC: duidLLT, IPv4: "0.0.0.0", IPv6: "2406::1", Hostname: "laptop", State: "active", Cltt: now - 300, ValidLft: 3600},
		{MAC: duidUUID, IPv4: "0.0.0.0", IPv6: "2406::2", State: "active", Cltt: now - 200, ValidLft: 3600},
		{MAC: duidMapped, IPv4: "0.0.0.0", IPv6: "2406::3", State: "active", Cltt: now - 100, ValidLft: 3600},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 台，实际 %d: %+v", len(got), got)
	}
	byID := map[string]DiscoveredTerminal{}
	for _, d := range got {
		byID[d.Identity] = d
		if d.IPv4 != "" {
			t.Errorf("0.0.0.0 不应作为地址展示: %+v", d)
		}
	}
	if d, ok := byID["70:08:94:31:cc:b5"]; !ok || d.DUID != duidLLT || d.IPv6 != "2406::1" {
		t.Fatalf("DUID-LLT 未解析为 MAC: %+v", got)
	}
	if d, ok := byID["aa:bb:cc:dd:ee:ff"]; !ok || d.DUID != duidMapped {
		t.Fatalf("映射表未生效: %+v", got)
	}
	if d, ok := byID[duidUUID]; !ok || d.MAC != "" {
		t.Fatalf("DUID-UUID 应以 DUID 为身份键: %+v", got)
	}
}

func TestDiscoverMergesDualStack(t *testing.T) {
	svc := NewAssetService(NewMemAssetRepo(), NewMemOrgStore())
	// 同一设备两条绑定（MAC 行 + DUID 行，映射表把 DUID 归并到同一 MAC）。
	duid := "000100012f51e6c570089431ccb5"
	svc.SetIdentityMapping(func(context.Context) (map[string]string, error) {
		return map[string]string{duid: "70:08:94:31:cc:b5"}, nil
	})
	now := time.Now().Unix()
	got, err := svc.Discover(context.Background(), []LedgerBinding{
		{MAC: "70:08:94:31:cc:b5", IPv4: "10.61.67.10", Hostname: "pc", State: "active",
			Cltt: now - 200, ValidLft: 3600, SubnetID: "s1", SubnetCIDR: "10.61.67.0/24", OrgID: "o1"},
		{MAC: duid, IPv4: "0.0.0.0", IPv6: "2406::10", State: "grace",
			Cltt: now - 100, ValidLft: 3600},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("v4/v6 应合并为一条，实际 %d: %+v", len(got), got)
	}
	d := got[0]
	if d.IPv4 != "10.61.67.10" || d.IPv6 != "2406::10" || d.Hostname != "pc" {
		t.Fatalf("地址/主机名应取并集: %+v", d)
	}
	if d.State != "grace" {
		t.Fatalf("状态应取最严重（grace）: %+v", d)
	}
	if d.SubnetID != "s1" || d.OrgID != "o1" {
		t.Fatalf("子网/组织上下文丢失: %+v", d)
	}
	if d.LastSeen.Unix() != now-100 {
		t.Fatalf("lastSeen 应取最近一次租约: %v", d.LastSeen)
	}
}

func TestDiscoverSkipsExpiredAndRegistered(t *testing.T) {
	repo := NewMemAssetRepo()
	_ = repo.Upsert(context.Background(), Asset{MAC: "aa:bb:cc:dd:ee:01", Owner: "已登记"})
	svc := NewAssetService(repo, NewMemOrgStore())
	now := time.Now()
	got, err := svc.Discover(context.Background(), []LedgerBinding{
		// 已登记
		{MAC: "aa:bb:cc:dd:ee:01", IPv4: "10.0.0.1", State: "active", ValidLft: 3600},
		// 状态 active 但租期已过 → 不计
		{MAC: "aa:bb:cc:dd:ee:02", IPv4: "10.0.0.2", State: "active",
			Cltt: now.Add(-2 * time.Hour).Unix(), ValidLft: 3600},
		// 非 active/grace 状态 → 不计
		{MAC: "aa:bb:cc:dd:ee:03", IPv4: "10.0.0.3", State: "expired", ValidLft: 3600},
		// 有效 grace → 计入
		{MAC: "aa:bb:cc:dd:ee:04", IPv4: "10.0.0.4", State: "grace",
			Cltt: now.Add(-time.Minute).Unix(), ValidLft: 3600},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].MAC != "aa:bb:cc:dd:ee:04" || got[0].State != "grace" {
		t.Fatalf("过期/非活跃/已登记都应剔除: %+v", got)
	}
}

func TestAssetListExpandsOrgSubtree(t *testing.T) {
	// M6-047：资产页组织筛选按子树展开，与日志中心口径一致。
	const root, child, other = "11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222", "33333333-3333-3333-3333-333333333333"
	orgs := NewMemOrgStore()
	_ = orgs.Create(OrgNode{ID: root, Name: "总部", Path: "/" + root})
	_ = orgs.Create(OrgNode{ID: child, ParentID: root, Name: "研发", Path: "/" + root + "/" + child})
	_ = orgs.Create(OrgNode{ID: other, Name: "其他", Path: "/" + other})

	svc := NewAssetService(NewMemAssetRepo(), orgs)
	for i, orgID := range []string{root, child, other} {
		mac := "aa:bb:cc:dd:ee:0" + fmt1(i+1)
		if _, err := svc.Upsert(context.Background(), Asset{MAC: mac, Owner: "张三", OrgID: orgID}); err != nil {
			t.Fatal(err)
		}
	}

	list, err := svc.List(context.Background(), root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("根节点应含自身+子节点共 2 条，实际 %d", len(list))
	}
	if list, _ = svc.List(context.Background(), child, ""); len(list) != 1 || list[0].OrgID != child {
		t.Fatalf("子节点应只含自身 1 条: %+v", list)
	}
	// 不存在的组织节点 → 空结果（不能退化为全量）。
	if list, _ = svc.List(context.Background(), "99999999-9999-9999-9999-999999999999", ""); len(list) != 0 {
		t.Fatalf("未知组织应返回空，实际 %d 条", len(list))
	}
}

func TestDiscoverSkipsIgnored(t *testing.T) {
	// 忽略名单按 MAC / DUID 双键生效：先 DUID-only 忽略 → 解析出 MAC 后仍被抑制。
	svc := NewAssetService(NewMemAssetRepo(), NewMemOrgStore())
	ignored := NewMemIgnoredRepo()
	svc.SetIgnoredRepo(ignored)
	duid := "000100012f51e6c570089431ccb5"
	if _, err := svc.Ignore(context.Background(), "", duid, "打印机"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	got, err := svc.Discover(context.Background(), []LedgerBinding{
		{MAC: duid, IPv4: "0.0.0.0", IPv6: "2406::9", State: "active", Cltt: now - 60, ValidLft: 3600},
		{MAC: "aa:bb:cc:dd:ee:01", IPv4: "10.0.0.1", State: "active", Cltt: now - 30, ValidLft: 3600},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].MAC != "aa:bb:cc:dd:ee:01" {
		t.Fatalf("被忽略终端不应出现: %+v", got)
	}
	if list, _ := svc.ListIgnored(context.Background()); len(list) != 1 || list[0].Reason != "打印机" {
		t.Fatalf("忽略名单读取异常: %+v", list)
	}
	if err := svc.Unignore(context.Background(), duid); err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	if err := svc.Unignore(context.Background(), duid); !errors.Is(err, ErrIgnoredNotFound) {
		t.Fatalf("重复恢复应 404: %v", err)
	}
}

func TestBulkUpsertPartialSuccess(t *testing.T) {
	svc := NewAssetService(NewMemAssetRepo(), NewMemOrgStore())
	saved, failures := svc.BulkUpsert(context.Background(), []Asset{
		{MAC: "aa:bb:cc:dd:ee:01", Owner: "张三"},
		{MAC: "zz:zz", Owner: "李四"}, // 非法 MAC
		{DUID: "000100012df9b6d9f8a2d6c97867", Owner: "王五"},
		{MAC: "aa:bb:cc:dd:ee:02"}, // 缺使用人
	})
	if saved != 2 || len(failures) != 2 {
		t.Fatalf("部分成功语义不符: saved=%d failures=%+v", saved, failures)
	}
	if failures[0].Index != 1 || failures[0].Code != "BAD_MAC" {
		t.Fatalf("首条失败应 index=1/BAD_MAC: %+v", failures[0])
	}
	if failures[1].Index != 3 || failures[1].Code != "OWNER_REQUIRED" {
		t.Fatalf("第二条失败应 index=3/OWNER_REQUIRED: %+v", failures[1])
	}
}

func TestAssetLeaseIndex(t *testing.T) {
	// 在线口径：只认 active 且未过期；grace/conflict 展示状态但不计在线。
	duid := "000100012f51e6c570089431ccb5"
	now := time.Now().Unix()
	idx := AssetLeaseIndex([]LedgerBinding{
		{MAC: "aa:bb:cc:dd:ee:01", IPv4: "10.0.0.10", State: "active", Cltt: now - 60, ValidLft: 3600},
		{MAC: "aa:bb:cc:dd:ee:02", IPv4: "10.0.0.11", State: "grace", Cltt: now - 60, ValidLft: 3600},
		{MAC: "aa:bb:cc:dd:ee:03", IPv4: "10.0.0.12", State: "active", Cltt: now - 7200, ValidLft: 3600},
		{MAC: duid, IPv4: "0.0.0.0", IPv6: "2406::20", State: "conflict", Cltt: now - 30, ValidLft: 3600},
	}, map[string]string{duid: "aa:bb:cc:dd:ee:04"})

	if l := idx["aa:bb:cc:dd:ee:01"]; !l.Online || l.IPv4 != "10.0.0.10" {
		t.Fatalf("active 应在线且带地址: %+v", l)
	}
	if l := idx["aa:bb:cc:dd:ee:02"]; l.Online || l.State != "grace" {
		t.Fatalf("grace 不应计在线: %+v", l)
	}
	if l := idx["aa:bb:cc:dd:ee:03"]; l.Online {
		t.Fatalf("租期已过不应在线: %+v", l)
	}
	if l := idx["aa:bb:cc:dd:ee:04"]; l.Online || l.IPv6 != "2406::20" || l.State != "conflict" {
		t.Fatalf("DUID 绑定应归并到 MAC 身份且保留 conflict: %+v", l)
	}
	if l := idx[duid]; l.IPv6 != "2406::20" {
		t.Fatalf("DUID 别名应可命中: %+v", l)
	}
}

func TestAssetListPaginationAndStats(t *testing.T) {
	// M6-048：服务端分页 + 统计不受分页影响；在线按身份键集合过滤。
	const orgA, orgB = "11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222"
	orgs := NewMemOrgStore()
	_ = orgs.Create(OrgNode{ID: orgA, Name: "A", Path: "/" + orgA})
	_ = orgs.Create(OrgNode{ID: orgB, Name: "B", Path: "/" + orgB})
	repo := NewMemAssetRepo()
	svc := NewAssetService(repo, orgs)
	for i := 0; i < 25; i++ {
		mac := "aa:bb:cc:dd:ee:" + fmt1(10+i)
		a := Asset{MAC: mac, Owner: "张三", Dept: "研发"}
		if i%2 == 0 {
			a.OrgID = orgA
		}
		if i%3 == 0 {
			a.Tags = []string{"server"}
		}
		if _, err := svc.Upsert(context.Background(), a); err != nil {
			t.Fatal(err)
		}
	}
	onlineIDs := []string{"aa:bb:cc:dd:ee:10", "aa:bb:cc:dd:ee:12"}

	items, stats, err := svc.ListPage(context.Background(), AssetQuery{
		Limit: 10, Offset: 0, OnlineIDs: onlineIDs,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 10 || stats.Total != 25 {
		t.Fatalf("首页应 10 条、统计 25 条: items=%d stats=%+v", len(items), stats)
	}
	if stats.Assigned != 13 || stats.Unassigned != 12 {
		t.Fatalf("组织归属统计不符: %+v", stats)
	}
	if stats.Owners != 1 || stats.Depts != 1 {
		t.Fatalf("使用人/部门去重不符: %+v", stats)
	}
	if stats.Online != 2 || stats.Offline != 23 {
		t.Fatalf("在线统计不符: %+v", stats)
	}
	if stats.Tagged != 9 {
		t.Fatalf("标签统计不符: %+v", stats)
	}

	// 第 3 页只剩 5 条，统计仍是全量 25。
	items, stats, _ = svc.ListPage(context.Background(), AssetQuery{Limit: 10, Offset: 20})
	if len(items) != 5 || stats.Total != 25 {
		t.Fatalf("末页应 5 条、统计仍 25: items=%d stats.Total=%d", len(items), stats.Total)
	}

	// 在线过滤：只返回在线集合内的资产，统计随之收敛。
	onlineTrue := true
	items, stats, _ = svc.ListPage(context.Background(), AssetQuery{
		Limit: 10, OnlineIDs: onlineIDs, Online: &onlineTrue,
	})
	if len(items) != 2 || stats.Online != 2 || stats.Total != 2 {
		t.Fatalf("online=true 过滤不符: items=%d stats=%+v", len(items), stats)
	}

	// 未归属组织 + 已打标签。
	_, stats, _ = svc.ListPage(context.Background(), AssetQuery{Scope: "unassigned"})
	if stats.Total != 12 || stats.Assigned != 0 {
		t.Fatalf("unassigned 统计不符: %+v", stats)
	}
	_, stats, _ = svc.ListPage(context.Background(), AssetQuery{Scope: "tagged"})
	if stats.Total != 9 || stats.Tagged != 9 {
		t.Fatalf("tagged 统计不符: %+v", stats)
	}

	// 组织子树 + 分页组合：A 节点 13 条，第 2 页（10 起）3 条。
	items, stats, _ = svc.ListPage(context.Background(), AssetQuery{OrgID: orgA, Limit: 10, Offset: 10})
	if stats.Total != 13 || len(items) != 3 {
		t.Fatalf("子树分页不符: items=%d stats=%+v", len(items), stats)
	}
}

func TestTerminalStats(t *testing.T) {
	// 仪表盘 KPI 与发现页同口径：未登记 = 有效租约 - 已登记 - 忽略。
	svc := NewAssetService(NewMemAssetRepo(), NewMemOrgStore())
	svc.SetIgnoredRepo(NewMemIgnoredRepo())
	now := time.Now().Unix()
	bindings := []LedgerBinding{
		{MAC: "aa:bb:cc:dd:ee:01", IPv4: "10.0.0.1", State: "active", Cltt: now - 10, ValidLft: 3600},
		{MAC: "aa:bb:cc:dd:ee:02", IPv4: "10.0.0.2", State: "active", Cltt: now - 10, ValidLft: 3600},
		{MAC: "aa:bb:cc:dd:ee:03", IPv4: "10.0.0.3", State: "active", Cltt: now - 10, ValidLft: 3600},
		// 已登记（下面 Upsert）
		// 忽略（下面 Ignore）
		// 过期 → 不计未登记
		{MAC: "aa:bb:cc:dd:ee:04", IPv4: "10.0.0.4", State: "active",
			Cltt: now - 7200, ValidLft: 3600},
	}
	if _, err := svc.Upsert(context.Background(), Asset{MAC: "aa:bb:cc:dd:ee:01", Owner: "张三"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Ignore(context.Background(), "aa:bb:cc:dd:ee:02", "", "打印机"); err != nil {
		t.Fatal(err)
	}
	st, err := svc.TerminalStats(context.Background(), bindings)
	if err != nil {
		t.Fatal(err)
	}
	if st.Unregistered != 1 || st.Ignored != 1 || st.Assets != 1 {
		t.Fatalf("终端概览不符: %+v", st)
	}
}
