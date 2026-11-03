package ipam

import (
	"context"
	"errors"
	"testing"
)

type fakeKea struct {
	deployed []Subnet
	failNext bool
	removed  []int
}

func (f *fakeKea) DeploySubnet(_ context.Context, ss []Subnet, dry bool) (int, error) {
	if f.failNext {
		f.failNext = false
		return 0, errors.New("kea down")
	}
	if !dry {
		f.deployed = append(f.deployed, ss...)
	}
	return 1, nil
}
func (f *fakeKea) RemoveSubnet(_ context.Context, id int) error {
	f.removed = append(f.removed, id)
	return nil
}
func (f *fakeKea) ReserveAddress(_ context.Context, _, _ string) error { return nil }
func (f *fakeKea) BindStatic(_ context.Context, _, _, _ string) error  { return nil }

func newSubnetSvc(t *testing.T) (*SubnetService, *fakeKea, *MemOrgStore) {
	t.Helper()
	orgs := NewMemOrgStore()
	kea := &fakeKea{}
	svc := NewSubnetService(NewMemSubnetRepo(), orgs, kea, nil)
	return svc, kea, orgs
}

func TestSubnetCreate_dryRun不下发不落库(t *testing.T) {
	svc, kea, _ := newSubnetSvc(t)
	in := Subnet{OrgID: "", Name: "办公", Family: 4, CIDR: "10.1.0.0/24",
		Pools: []Pool{{StartAddr: "10.1.0.10", EndAddr: "10.1.0.100", Kind: "dynamic"}}}
	out, err := svc.Create(context.Background(), in, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(kea.deployed) != 0 {
		t.Fatalf("dryRun must not call engine, got %d deploys", len(kea.deployed))
	}
	if out.KeaSubnetID == 0 {
		t.Fatal("dryRun should still carry kea subnet id")
	}
}

func TestSubnetCreate_引擎失败不落库(t *testing.T) {
	svc, kea, _ := newSubnetSvc(t)
	kea.failNext = true
	in := Subnet{Name: "办公", Family: 4, CIDR: "10.1.0.0/24"}
	if _, err := svc.Create(context.Background(), in, false); !errors.Is(err, ErrKeaDown) {
		t.Fatalf("err=%v want KEA_DOWN", err)
	}
	if len(kea.removed) != 0 {
		t.Fatalf("no kea id returned, nothing to remove; got %v", kea.removed)
	}
}

func TestSubnetCreate_正常落库带keaID(t *testing.T) {
	svc, _, _ := newSubnetSvc(t)
	out, err := svc.Create(context.Background(), Subnet{Name: "办公", Family: 4, CIDR: "10.2.0.0/24"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if out.ID == "" || out.KeaSubnetID != 1 {
		t.Fatalf("unexpected: %+v", out)
	}
}

func TestSubnetUpdate_引擎失败回滚DB(t *testing.T) {
	svc, kea, _ := newSubnetSvc(t)
	created, err := svc.Create(context.Background(), Subnet{Name: "办公", Family: 4, CIDR: "10.3.0.0/24"}, false)
	if err != nil {
		t.Fatal(err)
	}
	kea.failNext = true
	_, err = svc.Update(context.Background(), created.ID, Subnet{Name: "办公-改名", Pools: []Pool{{StartAddr: "10.3.0.2", EndAddr: "10.3.0.9", Kind: "dynamic"}}})
	if !errors.Is(err, ErrKeaDown) {
		t.Fatalf("err=%v want KEA_DOWN", err)
	}
	after, ok, _ := svc.repo.Get(context.Background(), created.ID)
	if !ok || after.Name != "办公" {
		t.Fatalf("DB not rolled back: %+v", after)
	}
}

func TestSubnetCIDR族校验(t *testing.T) {
	svc, _, _ := newSubnetSvc(t)
	if _, err := svc.Create(context.Background(), Subnet{Family: 6, CIDR: "10.4.0.0/24"}, false); !errors.Is(err, ErrFamilyMismatch) {
		t.Fatalf("err=%v want FAMILY_MISMATCH", err)
	}
	if _, err := svc.Create(context.Background(), Subnet{Family: 4, CIDR: "bad"}, false); !errors.Is(err, ErrBadCIDR) {
		t.Fatalf("err=%v want BAD_CIDR", err)
	}
}

func TestSubnetCIDR归一化(t *testing.T) {
	cases := []struct{ in, want string }{
		{"2406:440:3C16:4005:10:61:40:1/112", "2406:440:3c16:4005:10:61:40:0/112"},
		{"2406:440:3C16:4005:10:61:40:1/112 ", "2406:440:3c16:4005:10:61:40:0/112"},
		{" 2406:440:3C16:4005:10:61:40:1/112", "2406:440:3c16:4005:10:61:40:0/112"},
		{"2406:440:3C16:4005:10:61:40:1/112\n", "2406:440:3c16:4005:10:61:40:0/112"},
		{"2406:440:3c16:4006::/64", "2406:440:3c16:4006::/64"},
		{"10.61.40.5/24", "10.61.40.0/24"},
	}
	for _, c := range cases {
		got, err := normalizeCIDR(c.in)
		if err != nil {
			t.Fatalf("normalizeCIDR(%q) err=%v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("normalizeCIDR(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	for _, bad := range []string{"", "   ", "bad", "10.0.0.0", "10.0.0.0/33", "2406:440:3c16:4006::/129"} {
		if _, err := normalizeCIDR(bad); !errors.Is(err, ErrBadCIDR) {
			t.Fatalf("normalizeCIDR(%q) err=%v want BAD_CIDR", bad, err)
		}
	}
}

func TestSubnetCreate_空白与非规范CIDR落库为规范值(t *testing.T) {
	svc, _, _ := newSubnetSvc(t)
	saved, err := svc.Create(context.Background(), Subnet{Family: 6, CIDR: " 2406:440:3C16:4005:10:61:40:1/112 "}, false)
	if err != nil {
		t.Fatalf("create err=%v", err)
	}
	if saved.CIDR != "2406:440:3c16:4005:10:61:40:0/112" {
		t.Fatalf("stored CIDR = %q", saved.CIDR)
	}
	// 归一化后重复判定生效：非规范写法应命中 SUBNET_DUP
	if _, err := svc.Create(context.Background(), Subnet{Family: 6, CIDR: "2406:440:3c16:4005:10:61:40:0/112"}, false); !errors.Is(err, ErrSubnetDup) {
		t.Fatalf("err=%v want SUBNET_DUP", err)
	}
}

// M6-022：子网级元数据（VLAN/站点/用途/负责人）落库、更新与契约映射。
func TestSubnetMetaRoundTrip(t *testing.T) {
	svc, _, _ := newSubnetSvc(t)
	in := Subnet{
		Name: "研发网段", Family: 4, CIDR: "10.62.0.0/24",
		VLAN: "100", Site: "A 栋 3F", Purpose: "办公终端", Owner: "张三",
	}
	created, err := svc.Create(context.Background(), in, false)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, _ := svc.repo.Get(context.Background(), created.ID)
	if !ok {
		t.Fatal("not found")
	}
	if got.VLAN != "100" || got.Site != "A 栋 3F" || got.Purpose != "办公终端" || got.Owner != "张三" {
		t.Fatalf("元数据未落库: %+v", got)
	}

	// 更新：仅改 VLAN/负责人，其余保留。
	upd := got
	upd.VLAN = "200"
	upd.Owner = "李四"
	next, err := svc.Update(context.Background(), created.ID, upd)
	if err != nil {
		t.Fatal(err)
	}
	if next.VLAN != "200" || next.Owner != "李四" || next.Site != "A 栋 3F" {
		t.Fatalf("更新结果异常: %+v", next)
	}

	// 契约映射：空字段转指针非 nil 但值为空串，前端显示占位。
	g := toGenSubnet(next)
	if g.Vlan == nil || *g.Vlan != "200" || g.Owner == nil || *g.Owner != "李四" {
		t.Fatalf("契约映射异常: %+v", g)
	}
}
