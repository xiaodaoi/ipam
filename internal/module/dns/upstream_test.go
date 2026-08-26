package dns

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestProber_三连败摘除二连胜回切(t *testing.T) {
	fail := 0
	dial := func(context.Context, string) (time.Duration, error) {
		if fail >= 2 {
			return 0, errors.New("down")
		}
		fail++
		return 5 * time.Millisecond, nil
	}
	p := NewProber(time.Second, dial)

	ups := []Upstream{{ID: "u1", Name: "a", Addrs: []string{"1.1.1.1"}, Enabled: true}}

	// 连续 3 次失败 → down
	dial = func(context.Context, string) (time.Duration, error) { return 0, errors.New("down") }
	p.dial = dial
	for i := 0; i < 3; i++ {
		p.ProbeOnce(context.Background(), ups)
	}
	if p.Status("u1").Up {
		t.Fatal("expect down after 3 fails")
	}
	if p.Status("u1").ConsecutiveFails != 3 {
		t.Fatalf("fails=%d", p.Status("u1").ConsecutiveFails)
	}

	// 2 次成功 → up
	p.dial = func(context.Context, string) (time.Duration, error) { return 2 * time.Millisecond, nil }
	for i := 0; i < 2; i++ {
		p.ProbeOnce(context.Background(), ups)
	}
	if !p.Status("u1").Up || p.Status("u1").RTTMs != 2 {
		t.Fatalf("expect up with rtt=2ms: %+v", p.Status("u1"))
	}
}

func TestService_创建触发下发与状态合并(t *testing.T) {
	repo := NewMemUpstreamRepo()
	ctl := &fakeCtl{}
	p := NewProber(time.Second, func(context.Context, string) (time.Duration, error) { return 1 * time.Millisecond, nil })
	svc := NewService(repo, p, ctl)

	u, err := svc.Create(context.Background(), Upstream{Name: "a", Addrs: []string{"223.5.5.5"}, Protocol: "udp", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if ctl.synced != 1 {
		t.Fatalf("forward sync not triggered: %d", ctl.synced)
	}
	p.ProbeOnce(context.Background(), []Upstream{u})
	list, health, _ := svc.List(context.Background())
	if len(list) != 1 || !health[0].Up {
		t.Fatalf("list/health: %+v %+v", list, health)
	}
}

type fakeCtl struct{ synced, ruleSyncs, zoneReloads int }

func (f *fakeCtl) SyncForward(context.Context, []Upstream) error {
	f.synced++
	return nil
}
func (f *fakeCtl) SyncForwardRules(context.Context, []ForwardRule, []Upstream) error {
	f.ruleSyncs++
	return nil
}
func (f *fakeCtl) AuthZoneReload(context.Context, string) error {
	f.zoneReloads++
	return nil
}
