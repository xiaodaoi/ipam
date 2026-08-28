package ipam

import (
	"context"
	"testing"
)

func newBulkSvc() (*LedgerService, *MemReservationRepo) {
	repo := NewMemReservationRepo()
	svc := NewLedgerService(func(context.Context) LedgerSource { return LedgerSource{} },
		repo, NewMemSubnetRepo(), nil)
	return svc, repo
}

func TestBulkReservations_全部成功(t *testing.T) {
	svc, repo := newBulkSvc()
	res, err := svc.BulkReservations(context.Background(), "s1", []BulkEntry{
		{Kind: "reserve", Address: "10.1.0.10"},
		{Kind: "bind", Address: "10.1.0.20", MAC: "aa:bb:cc:dd:ee:02"},
	})
	if err != nil || !res.OK || res.Applied != 2 || len(res.Failures) != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	list, _ := repo.List(context.Background())
	if len(list) != 2 {
		t.Fatalf("applied count: %d", len(list))
	}
}

func TestBulkReservations_占用行导致整体回滚(t *testing.T) {
	repo := NewMemReservationRepo()
	svc := NewLedgerService(func(context.Context) LedgerSource {
		return LedgerSource{Bindings: []LedgerBinding{{IPv4: "10.1.0.10", State: "active"}}}
	}, repo, NewMemSubnetRepo(), nil)

	res, _ := svc.BulkReservations(context.Background(), "s1", []BulkEntry{
		{Kind: "reserve", Address: "10.1.0.11"}, // 本可成功
		{Kind: "reserve", Address: "10.1.0.10"}, // 占用 → 整体失败
	})
	if res.OK || len(res.Failures) != 1 || res.Failures[0].Line != 2 {
		t.Fatalf("res=%+v", res)
	}
	// 回滚验证：无任何写入（第 1 行也不得保留）
	list, _ := repo.List(context.Background())
	if len(list) != 0 {
		t.Fatalf("rollback incomplete: %+v", list)
	}
}

func TestBulkReservations_非法MAC(t *testing.T) {
	svc, _ := newBulkSvc()
	res, _ := svc.BulkReservations(context.Background(), "s1", []BulkEntry{
		{Kind: "bind", Address: "10.1.0.30", MAC: "zz-not-mac"},
	})
	if res.OK || len(res.Failures) != 1 || res.Failures[0].Reason == "" {
		t.Fatalf("res=%+v", res)
	}
}
