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

func (f *fakeKea) DeploySubnet(_ context.Context, ss []Subnet, _ bool) (int, error) {
	if f.failNext {
		f.failNext = false
		return 0, errors.New("kea down")
	}
	f.deployed = append(f.deployed, ss...)
	return 1, nil
}
func (f *fakeKea) RemoveSubnet(_ context.Context, id int) error {
	f.removed = append(f.removed, id)
	return nil
}

func newSubnetSvc(t *testing.T) (*SubnetService, *fakeKea, *MemOrgStore) {
	t.Helper()
	orgs := NewMemOrgStore()
	kea := &fakeKea{}
	svc := NewSubnetService(NewMemSubnetRepo(), orgs, kea)
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
