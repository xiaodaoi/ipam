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
