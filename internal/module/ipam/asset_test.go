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
	list, _ := repo.List(context.Background(), "", "")
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

	if list, _ := repo.List(context.Background(), "", "李四"); len(list) != 1 {
		t.Fatalf("q filter: %+v", list)
	}
	if list, _ := repo.List(context.Background(), "", "财务"); len(list) != 1 {
		t.Fatalf("dept filter: %+v", list)
	}
	if err := svc.Delete(context.Background(), "AA:BB:CC:DD:EE:01"); err != nil {
		t.Fatal(err)
	}
	if list, _ := repo.List(context.Background(), "", ""); len(list) != 1 || strings.Contains(list[0].MAC, "01") {
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
		if list, err := repo.List(context.Background(), "", q); err != nil || len(list) != 1 {
			t.Fatalf("q=%q 应命中 1 条，实际 %d (%v)", q, len(list), err)
		}
	}
	if err := svc.Delete(context.Background(), "aa:bb:cc:dd:ee:99"); !errors.Is(err, ErrAssetNotFound) {
		t.Fatalf("删除不存在的资产应报 404: %v", err)
	}
	if err := svc.Delete(context.Background(), "688FC95B39CF"); err != nil {
		t.Fatalf("省略分隔符删除应成功: %v", err)
	}
	if list, _ := repo.List(context.Background(), "", ""); len(list) != 0 {
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
