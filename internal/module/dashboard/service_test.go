package dashboard

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/xiaodaoi/ipam/internal/module/ipam"
	"github.com/xiaodaoi/ipam/internal/module/logquery"
)

func fixedNow() time.Time {
	return time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)
}

func seedLogs(t *testing.T, now time.Time) logquery.Store {
	t.Helper()
	m := logquery.NewMemStore()
	rows := []logquery.LogRow{}
	// 今日 DHCP：3 个不同 IP（其中 macA 今天首见）
	for i, ip := range []string{"10.61.172.1", "10.61.172.2", "10.61.172.3"} {
		rows = append(rows, logquery.LogRow{
			TS: now.Add(-time.Duration(i+1) * time.Minute), Type: "dhcp",
			ClientMAC: "aabbccddee0" + string(rune('a'+i)), ClientIP: ip,
		})
	}
	// 昨日活跃、今日未见 → 离线
	rows = append(rows, logquery.LogRow{TS: now.Add(-30 * time.Hour), Type: "dhcp",
		ClientMAC: "offline01", ClientIP: "10.61.173.9"})
	// 三天前首见且今日仍活动 → 非新增
	rows = append(rows, logquery.LogRow{TS: now.Add(-72 * time.Hour), Type: "dhcp",
		ClientMAC: "oldtimer", ClientIP: "10.61.172.1"})
	// DNS 查询 ×6（近 5 分钟内）→ qps5m=6/300
	for i := 0; i < 6; i++ {
		rows = append(rows, logquery.LogRow{TS: now.Add(-time.Duration(i) * time.Second),
			Type: "dns", Domain: "x.corp.local.", Action: "dns_query"})
	}
	// 拦截 ×2（今日）
	for i := 0; i < 2; i++ {
		rows = append(rows, logquery.LogRow{TS: now.Add(-time.Duration(i+1) * time.Minute),
			Type: "dns", Domain: "bad.example.", Action: "blocked"})
	}
	m.Append(rows...)
	return m
}

func testService(t *testing.T, now time.Time) (*Service, *Overview, error) {
	t.Helper()
	svc := NewService(seedLogs(t, now),
		func(context.Context) []ipam.Subnet {
			return []ipam.Subnet{
				{ID: "44444444-4444-4444-4444-444444444444", Name: "研发-办公",
					CIDR: "10.61.172.0/24", Pools: []ipam.Pool{{StartAddr: "10.61.172.10", EndAddr: "10.61.172.19"}}},
				{ID: "55555555-5555-5555-5555-555555555555", Name: "坏池",
					CIDR: "10.62.0.0/16", Pools: []ipam.Pool{{StartAddr: "bad", EndAddr: "worse"}}},
			}
		},
		func(context.Context) ([]ipam.LedgerBinding, error) {
			return []ipam.LedgerBinding{
				{State: "active"}, {State: "active"}, {State: "active"}, {State: "conflict"},
			}, nil
		},
		Lights{
			Postgres:   func(context.Context) Light { return LightUp },
			ClickHouse: nil,
			Kea:        func(context.Context) Light { return LightDown },
		})
	svc.now = func() time.Time { return now }
	ov, err := svc.Overview(context.Background(), 5)
	return svc, ov, err
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func TestOverviewAggregates(t *testing.T) {
	now := fixedNow()
	_, ov, err := testService(t, now)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if !ov.TodayStart.Equal(now.Truncate(24 * time.Hour)) {
		t.Fatalf("todayStart=%v", ov.TodayStart)
	}
	if ov.OnlineNow != 3 {
		t.Fatalf("onlineNow=%d want 3", ov.OnlineNow)
	}
	if ov.NewTerminals == nil || *ov.NewTerminals != 3 {
		t.Fatalf("newTerminals=%v want 3", ov.NewTerminals)
	}
	if ov.OfflineTerminals == nil || *ov.OfflineTerminals != 1 {
		t.Fatalf("offlineTerminals=%v want 1", ov.OfflineTerminals)
	}
	wantQps := float64(8) / 300 // type=dns 全量口径：查询 6 + 拦截 2
	if ov.Qps5m == nil || absF(*ov.Qps5m-wantQps) > 1e-9 {
		t.Fatalf("qps5m=%v want %v", ov.Qps5m, wantQps)
	}
	if ov.BlockedToday == nil || *ov.BlockedToday != 2 {
		t.Fatalf("blockedToday=%v want 2", ov.BlockedToday)
	}
	if ov.CoherencePct == nil || *ov.CoherencePct != 75 {
		t.Fatalf("coherence=%v want 75", ov.CoherencePct)
	}
	if ov.HitRatePct != nil {
		t.Fatal("命中率应恒为 null（遗留②）")
	}
	if len(ov.OnlineTrend) != 24 {
		t.Fatalf("trend slots=%d want 24", len(ov.OnlineTrend))
	}
	if prev := ov.OnlineTrend[len(ov.OnlineTrend)-2]; prev.Count != 3 { // 8 点桶含今日活跃 3 IP
		t.Fatalf("小时趋势错位，got %d", prev.Count)
	}
	// 健康灯三态混合与 unknown 兜底
	if ov.Lights["postgres"] != LightUp || ov.Lights["kea"] != LightDown ||
		ov.Lights["clickhouse"] != LightUnknown || ov.Lights["unbound"] != LightUnknown {
		t.Fatalf("lights=%v", ov.Lights)
	}
}

func TestPoolTopUtilization(t *testing.T) {
	now := fixedNow()
	_, ov, err := testService(t, now)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if len(ov.PoolTop) != 1 { // 坏池被跳过
		t.Fatalf("pool rows=%d want 1", len(ov.PoolTop))
	}
	p := ov.PoolTop[0]
	if p.Capacity != 10 { // .10..19 = 10 地址
		t.Fatalf("capacity=%d want 10", p.Capacity)
	}
	// 今日活动 IP 全部在 .172 段但池从 .10 起 —— seed 的 .1/.2/.3 不落池 → used=0
	if p.Used != 0 || p.Pct != 0 {
		t.Fatalf("used=%d pct=%v want 0/0", p.Used, p.Pct)
	}
}

func TestPoolUsedCountsInRange(t *testing.T) {
	now := fixedNow()
	m := logquery.NewMemStore()
	m.Append(
		logquery.LogRow{TS: now.Add(-time.Minute), Type: "dhcp", ClientIP: "10.61.172.11"},
		logquery.LogRow{TS: now.Add(-2 * time.Minute), Type: "dhcp", ClientIP: "10.61.172.12"},
		logquery.LogRow{TS: now.Add(-3 * time.Minute), Type: "dhcp", ClientIP: "10.61.172.99"},
		logquery.LogRow{TS: now.Add(-40 * time.Hour), Type: "dhcp", ClientIP: "10.61.172.11"}, // 昨日不计
	)
	svc := NewService(m,
		func(context.Context) []ipam.Subnet {
			return []ipam.Subnet{{ID: "44444444-4444-4444-4444-444444444444", Name: "研发",
				CIDR: "10.61.172.0/24", Pools: []ipam.Pool{{StartAddr: "10.61.172.10", EndAddr: "10.61.172.20"}}}}
		}, nil, Lights{})
	svc.now = func() time.Time { return now }
	ov, err := svc.Overview(context.Background(), 5)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if len(ov.PoolTop) != 1 || ov.PoolTop[0].Used != 2 {
		t.Fatalf("区间外/昨日必须剔除: %+v", ov.PoolTop)
	}
	if c := ov.PoolTop[0].Capacity; c != 11 { // .10..20
		t.Fatalf("capacity=%d want 11", c)
	}
}
func TestPoolTopOrgName(t *testing.T) {
	now := fixedNow()
	m := logquery.NewMemStore()
	m.Append(logquery.LogRow{TS: now.Add(-time.Minute), Type: "dhcp", ClientIP: "10.61.172.11"})
	svc := NewService(m,
		func(context.Context) []ipam.Subnet {
			return []ipam.Subnet{{ID: "55555555-5555-5555-5555-555555555555", OrgID: "org-1", Name: "研发",
				CIDR: "10.61.172.0/24", Pools: []ipam.Pool{{StartAddr: "10.61.172.10", EndAddr: "10.61.172.20"}}}}
		}, nil, Lights{})
	svc.now = func() time.Time { return now }

	// 未注入解析器 → orgName 为空。
	ov, err := svc.Overview(context.Background(), 5)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if len(ov.PoolTop) != 1 || ov.PoolTop[0].OrgID != "org-1" || ov.PoolTop[0].OrgName != "" {
		t.Fatalf("默认应无部门名: %+v", ov.PoolTop)
	}

	// 注入解析器 → 解析到部门名。
	svc.SetOrgNameResolver(func(id string) string {
		if id == "org-1" {
			return "研发中心"
		}
		return ""
	})
	ov, err = svc.Overview(context.Background(), 5)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if len(ov.PoolTop) != 1 || ov.PoolTop[0].OrgName != "研发中心" {
		t.Fatalf("部门名解析失败: %+v", ov.PoolTop)
	}
}

func TestPoolTopIPv6AndEmptyIP(t *testing.T) {
	now := fixedNow()
	m := logquery.NewMemStore()
	m.Append(
		logquery.LogRow{TS: now.Add(-time.Minute), Type: "dhcp", ClientIP: "2001:db8::10"},
		logquery.LogRow{TS: now.Add(-2 * time.Minute), Type: "dhcp", ClientIP: "2001:db8::20"},
		logquery.LogRow{TS: now.Add(-3 * time.Minute), Type: "dhcp", ClientIP: "10.0.0.5"},
		logquery.LogRow{TS: now.Add(-4 * time.Minute), Type: "dhcp"}, // 无 IP 不计入在线数
	)
	svc := NewService(m,
		func(context.Context) []ipam.Subnet {
			return []ipam.Subnet{{ID: "66666666-6666-6666-6666-666666666666", Name: "v6",
				CIDR: "2001:db8::/64", Pools: []ipam.Pool{{StartAddr: "2001:db8::1", EndAddr: "2001:db8::ff"}}}}
		}, nil, Lights{})
	svc.now = func() time.Time { return now }
	ov, err := svc.Overview(context.Background(), 5)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if ov.OnlineNow != 3 {
		t.Fatalf("onlineNow=%d want 3（空 IP 不计）", ov.OnlineNow)
	}
	if len(ov.PoolTop) != 1 || ov.PoolTop[0].Used != 2 {
		t.Fatalf("v6 池计数异常: %+v", ov.PoolTop)
	}
}

func TestOverviewCacheTTL(t *testing.T) {
	now := fixedNow()
	m := logquery.NewMemStore()
	m.Append(logquery.LogRow{TS: now.Add(-time.Minute), Type: "dhcp", ClientIP: "10.61.172.1"})
	svc := NewService(m, func(context.Context) []ipam.Subnet { return nil }, nil, Lights{})
	svc.now = func() time.Time { return now }

	ov1, err := svc.Overview(context.Background(), 5)
	if err != nil || ov1.OnlineNow != 1 {
		t.Fatalf("first=%v err=%v", ov1, err)
	}
	// TTL 内新增数据应复用快照，避免每次轮询都打 CH。
	m.Append(logquery.LogRow{TS: now.Add(-time.Minute), Type: "dhcp", ClientIP: "10.61.172.2"})
	ov2, _ := svc.Overview(context.Background(), 5)
	if ov2.OnlineNow != 1 {
		t.Fatalf("缓存未命中，onlineNow=%d want 1", ov2.OnlineNow)
	}
	// TTL 归零后立即重算。
	svc.SetCacheTTL(0)
	ov3, _ := svc.Overview(context.Background(), 5)
	if ov3.OnlineNow != 2 {
		t.Fatalf("缓存关闭后 onlineNow=%d want 2", ov3.OnlineNow)
	}
}

func TestCountAddrInRange(t *testing.T) {
	addrs := []netip.Addr{
		netip.MustParseAddr("10.0.0.1"),
		netip.MustParseAddr("10.0.0.5"),
		netip.MustParseAddr("10.0.0.9"),
		netip.MustParseAddr("2001:db8::1"),
	}
	lo := netip.MustParseAddr("10.0.0.2")
	hi := netip.MustParseAddr("10.0.0.9")
	if n := countAddrInRange(addrs, lo, hi); n != 2 {
		t.Fatalf("in-range=%d want 2", n)
	}
	if n := countAddrInRange(addrs, netip.MustParseAddr("10.0.0.10"), netip.MustParseAddr("10.0.0.20")); n != 0 {
		t.Fatalf("empty-range=%d want 0", n)
	}
}
