package dns

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestProber_三连败标记异常一次成功恢复(t *testing.T) {
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

	// 连续 3 次失败 → 标记异常
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

	// 1 次成功 → 恢复
	p.dial = func(context.Context, string) (time.Duration, error) { return 2 * time.Millisecond, nil }
	p.ProbeOnce(context.Background(), ups)
	if !p.Status("u1").Up || p.Status("u1").RTTMs != 2 {
		t.Fatalf("expect up with rtt=2ms: %+v", p.Status("u1"))
	}
	if p.Status("u1").ConsecutiveFails != 0 {
		t.Fatalf("fails not reset: %+v", p.Status("u1"))
	}
}

func TestProber_多地址任一可达即在线(t *testing.T) {
	calls := 0
	dial := func(_ context.Context, addr string) (time.Duration, error) {
		calls++
		if addr == "1.1.1.1" {
			return 0, errors.New("unreachable")
		}
		return 7 * time.Millisecond, nil
	}
	p := NewProber(time.Second, dial)
	ups := []Upstream{{ID: "u1", Name: "multi", Addrs: []string{"1.1.1.1", "223.5.5.5"}, Enabled: true}}

	p.ProbeOnce(context.Background(), ups)
	h := p.Status("u1")
	if !h.Up || h.RTTMs != 7 || h.ConsecutiveFails != 0 {
		t.Fatalf("expect up via second addr: %+v", h)
	}
	if calls != 2 {
		t.Fatalf("expect probe all addrs, calls=%d", calls)
	}

	// 全部成功时 RTT 取最小值
	p.dial = func(_ context.Context, addr string) (time.Duration, error) {
		if addr == "1.1.1.1" {
			return 9 * time.Millisecond, nil
		}
		return 3 * time.Millisecond, nil
	}
	p.ProbeOnce(context.Background(), ups)
	if got := p.Status("u1").RTTMs; got != 3 {
		t.Fatalf("expect min rtt=3ms, got %d", got)
	}
}

func TestProber_连续失败计数封顶(t *testing.T) {
	p := NewProber(time.Second, func(context.Context, string) (time.Duration, error) {
		return 0, errors.New("down")
	})
	ups := []Upstream{{ID: "u1", Addrs: []string{"1.1.1.1"}, Enabled: true}}
	for i := 0; i < maxConsecutiveFails+50; i++ {
		p.ProbeOnce(context.Background(), ups)
	}
	if got := p.Status("u1").ConsecutiveFails; got != maxConsecutiveFails {
		t.Fatalf("expect capped at %d, got %d", maxConsecutiveFails, got)
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

type fakeCtl struct {
	synced, ruleSyncs, zoneReloads, reloads, localZones int
	localData                                           []string
	removedZones                                        []string
	zoneTypes                                           map[string]string
	flushes                                             []string
	serials                                             map[string]uint32
	// M6-038：模拟真实 unbound——热载后按 zonefile 实际内容回报 serial；
	// failReloadZone 指定某区热载失败（回滚路径测试）。
	zoneDir            string
	failReloadZone     string
	serialMismatchZone string
}

func (f *fakeCtl) SyncForward(context.Context, []Upstream) error {
	f.synced++
	return nil
}
func (f *fakeCtl) SyncForwardRules(context.Context, []ForwardRule, []Upstream) error {
	f.ruleSyncs++
	return nil
}
func (f *fakeCtl) AuthZoneReload(_ context.Context, zone string) error {
	if f.failReloadZone != "" && f.failReloadZone == zone {
		return fmt.Errorf("fake reload %s failed", zone)
	}
	f.zoneReloads++
	if f.zoneDir == "" {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(f.zoneDir, zone+".zone"))
	if err != nil {
		return nil
	}
	if serial, ok := parseSOASerial(string(b)); ok {
		if f.serials == nil {
			f.serials = map[string]uint32{}
		}
		f.serials[zone] = serial
	}
	if f.serialMismatchZone == zone {
		f.serials[zone] = 999999 // 模拟运行态与编译产物不一致
	}
	return nil
}

// parseSOASerial 从 zonefile 文本提取 SOA serial（测试辅助）。
func parseSOASerial(text string) (uint32, bool) {
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, " SOA ") {
			continue
		}
		fields := strings.Fields(line)
		// 形如：@ SOA localhost. hostmaster.localhost. 1794998065 3600 600 86400 300
		for i := 0; i+1 < len(fields); i++ {
			if strings.HasSuffix(fields[i], "hostmaster.localhost.") {
				if n, err := strconv.ParseUint(fields[i+1], 10, 32); err == nil {
					return uint32(n), true
				}
			}
		}
	}
	return 0, false
}
func (f *fakeCtl) LocalZone(_ context.Context, name, zt string) error {
	f.localZones++
	if f.zoneTypes == nil {
		f.zoneTypes = map[string]string{}
	}
	f.zoneTypes[name] = zt
	return nil
}
func (f *fakeCtl) LocalZoneRemove(_ context.Context, name string) error {
	f.removedZones = append(f.removedZones, name)
	return nil
}
func (f *fakeCtl) LocalData(_ context.Context, data string) error {
	f.localData = append(f.localData, data)
	return nil
}
func (f *fakeCtl) LocalDataRemove(context.Context, string) error   { return nil }
func (f *fakeCtl) CheckConf(context.Context, string, string) error { return nil }
func (f *fakeCtl) Reload(context.Context) error {
	f.reloads++
	return nil
}
func (f *fakeCtl) FlushZone(_ context.Context, name string) error {
	f.flushes = append(f.flushes, name)
	return nil
}
func (f *fakeCtl) AuthZoneSerials(context.Context) (map[string]uint32, error) {
	if f.serials == nil {
		return map[string]uint32{}, nil
	}
	out := make(map[string]uint32, len(f.serials))
	for k, v := range f.serials {
		out[k] = v
	}
	return out, nil
}
