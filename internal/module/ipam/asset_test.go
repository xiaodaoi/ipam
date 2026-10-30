package ipam

import (
	"context"
	"strings"
	"testing"
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
