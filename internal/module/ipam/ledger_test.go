package ipam

import (
	"context"
	"errors"
	"testing"
)

func TestClassify_六态判定矩阵(t *testing.T) {
	cases := []struct {
		name   string
		res    Reservation
		b      *LedgerBinding
		inPool bool
		want   LedgerState
	}{
		{"静态绑定-有MAC", Reservation{MAC: "aa", IPv4: "1.1.1.1"}, nil, true, StateStatic},
		{"保留-无MAC", Reservation{IPv4: "1.1.1.2"}, nil, true, StateReserved},
		{"在线-活跃", Reservation{}, &LedgerBinding{State: "active"}, true, StateOnline},
		{"宽限", Reservation{}, &LedgerBinding{State: "grace"}, true, StateGrace},
		{"冲突", Reservation{}, &LedgerBinding{State: "conflict"}, true, StateConflict},
		{"空闲-池内", Reservation{}, nil, true, StateAvailable},
	}
	for _, c := range cases {
		if got := Classify(c.res, c.b, c.inPool); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestQueryLedger_游标分页与状态过滤(t *testing.T) {
	src := LedgerSource{
		Subnets: []Subnet{{
			ID: "s1", Family: 4, CIDR: "10.1.0.0/24",
			Pools: []Pool{{StartAddr: "10.1.0.1", EndAddr: "10.1.0.5", Kind: "dynamic"}},
		}},
		Bindings: []LedgerBinding{{MAC: "aa:bb:cc:dd:ee:01", IPv4: "10.1.0.1", State: "active"}},
	}
	rows, next, total := QueryLedger(src, LedgerQuery{PageSize: 3})
	if len(rows) != 3 || total != 5 || next != "4:10.1.0.3" {
		t.Fatalf("page1: rows=%d total=%d next=%q", len(rows), total, next)
	}
	if rows[0].State != StateOnline || rows[1].State != StateAvailable {
		t.Fatalf("states wrong: %s %s", rows[0].State, rows[1].State)
	}
	// 第二页从游标续读
	rows2, next2, _ := QueryLedger(src, LedgerQuery{PageSize: 3, Cursor: next})
	if len(rows2) != 2 || next2 != "" {
		t.Fatalf("page2: rows=%d next=%q", len(rows2), next2)
	}
	// 状态过滤
	online, _, _ := QueryLedger(src, LedgerQuery{State: StateOnline})
	if len(online) != 1 || online[0].Address != "10.1.0.1" {
		t.Fatalf("online filter: %+v", online)
	}
}

func TestLedgerService_Reserve占用拒绝(t *testing.T) {
	src := func(context.Context) LedgerSource {
		return LedgerSource{Bindings: []LedgerBinding{{IPv4: "10.1.0.1", State: "active"}}}
	}
	svc := NewLedgerService(src, NewMemReservationRepo(), NewMemSubnetRepo(), nil)
	if err := svc.Reserve(context.Background(), "s1", "10.1.0.1"); !errors.Is(err, ErrAddrOccupied) {
		t.Fatalf("err=%v want ADDR_OCCUPIED", err)
	}
}

func TestLedgerService_BindStatic_成功后占位(t *testing.T) {
	repo := NewMemReservationRepo()
	svc := NewLedgerService(func(context.Context) LedgerSource { return LedgerSource{} },
		repo, NewMemSubnetRepo(), nil)
	if err := svc.BindStatic(context.Background(), "s1", "10.1.0.9", "aa:bb:cc:dd:ee:09"); err != nil {
		t.Fatal(err)
	}
	// 再次绑定同地址应被占用检查拒绝
	if err := svc.BindStatic(context.Background(), "s1", "10.1.0.9", "aa:bb:cc:dd:ee:0a"); !errors.Is(err, ErrAddrOccupied) {
		t.Fatalf("second bind err=%v want ADDR_OCCUPIED", err)
	}
}

func TestLedgerService_Release_释放回归可下发(t *testing.T) {
	repo := NewMemReservationRepo()
	svc := NewLedgerService(func(context.Context) LedgerSource { return LedgerSource{} },
		repo, NewMemSubnetRepo(), nil)
	ctx := context.Background()
	if err := svc.Reserve(ctx, "s1", "10.1.0.9"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Release(ctx, "10.1.0.9"); err != nil {
		t.Fatal(err)
	}
	rows, _ := repo.List(ctx)
	if len(rows) != 0 {
		t.Fatalf("释放后应无预留: %+v", rows)
	}
	// 释放不存在的地址 → 404 语义
	if err := svc.Release(ctx, "10.1.0.9"); !errors.Is(err, ErrAddrNotReserved) {
		t.Fatalf("err=%v want ADDR_NOT_RESERVED", err)
	}
}

func TestLedgerService_UpdateBinding_改绑与保留互转(t *testing.T) {
	repo := NewMemReservationRepo()
	svc := NewLedgerService(func(context.Context) LedgerSource { return LedgerSource{} },
		repo, NewMemSubnetRepo(), nil)
	ctx := context.Background()
	if err := svc.BindStatic(ctx, "s1", "10.1.0.9", "aa:bb:cc:dd:ee:09"); err != nil {
		t.Fatal(err)
	}
	// 改绑：MAC 更新且规范化为大写输入→小写冒号
	if err := svc.UpdateBinding(ctx, "10.1.0.9", "AA-BB-CC-DD-EE-0A"); err != nil {
		t.Fatal(err)
	}
	rows, _ := repo.List(ctx)
	if len(rows) != 1 || rows[0].MAC != "aa:bb:cc:dd:ee:0a" {
		t.Fatalf("改绑后: %+v", rows)
	}
	// 保留→绑定互转：无 MAC 预留写入 MAC
	if err := svc.Reserve(ctx, "s1", "10.1.0.10"); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateBinding(ctx, "10.1.0.10", "aa:bb:cc:dd:ee:0b"); err != nil {
		t.Fatal(err)
	}
	rows, _ = repo.List(ctx)
	for _, r := range rows {
		if r.IPv4 == "10.1.0.10" && r.MAC == "" {
			t.Fatal("保留写入 MAC 后应成为静态绑定")
		}
	}
	// 非法 MAC / 不存在的地址
	if err := svc.UpdateBinding(ctx, "10.1.0.9", "xy:zz"); !errors.Is(err, ErrBadMAC) {
		t.Fatalf("err=%v want BAD_MAC", err)
	}
	if err := svc.UpdateBinding(ctx, "10.9.9.9", "aa:bb:cc:dd:ee:0c"); !errors.Is(err, ErrAddrNotReserved) {
		t.Fatalf("err=%v want ADDR_NOT_RESERVED", err)
	}
}

func TestBulkReservations_apply失败整体回滚(t *testing.T) {
	// M3-007 核心回归：apply（kea config-set）失败时整体回滚，含 bind 行 Upsert
	// （修复 M2-017 发现的残留缺口：原实现回滚范围不含失败行自身）
	repo := NewMemReservationRepo()
	svc := NewLedgerService(func(context.Context) LedgerSource { return LedgerSource{} },
		repo, NewMemSubnetRepo(), func(context.Context) error { return errors.New("kea unavailable") })
	res, err := svc.BulkReservations(context.Background(), "s1", []BulkEntry{
		{Kind: "reserve", Address: "10.99.1.30"},
		{Kind: "bind", Address: "10.99.1.40", MAC: "aa:bb:cc:dd:ee:10"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatal("apply 失败应 ok:false")
	}
	rows, _ := repo.List(context.Background())
	if len(rows) != 0 {
		t.Fatalf("apply 失败应整体回滚（含 bind 行）: %+v", rows)
	}
}
