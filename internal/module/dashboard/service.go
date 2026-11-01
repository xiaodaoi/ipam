// Package dashboard 仪表盘聚合域（M4-004，F-01、§13.4 一级菜单）。
// 单端点聚合多源指标：logquery.Store 日志口径 + 子网/联动绑定来源 + 进程探针健康灯。
package dashboard

import (
	"context"
	"math/big"
	"net/netip"
	"sort"
	"sync"
	"time"

	"github.com/xiaodaoi/ipam/internal/module/ipam"
	"github.com/xiaodaoi/ipam/internal/module/logquery"
)

// Light 服务健康灯三态。
type Light string

const (
	LightUp      Light = "up"
	LightDown    Light = "down"
	LightUnknown Light = "unknown"
)

// Lights 健康灯探针集；nil 函数=unknown。
type Lights struct {
	Postgres   func(context.Context) Light
	ClickHouse func(context.Context) Light
	Kea        func(context.Context) Light
	Unbound    func(context.Context) Light
}

func (l Lights) of(ctx context.Context, f func(context.Context) Light) Light {
	if f == nil {
		return LightUnknown
	}
	return f(ctx)
}

// SubnetSource 子网快照回调（main 按 PG/Mem 装配）。
type SubnetSource func(ctx context.Context) []ipam.Subnet

// BindingSource 联动绑定快照回调。
type BindingSource func(ctx context.Context) ([]ipam.LedgerBinding, error)

// Service 聚合编排。
type Service struct {
	logs     logquery.Store
	subnets  SubnetSource
	bindings BindingSource
	lights   Lights
	now      func() time.Time // 测试注入

	// 快照缓存：仪表盘按 30s 轮询 + 多管理员并发，命中 TTL 内直接复用，
	// 避免每次请求都打满 ClickHouse（一次聚合约 5 条 CH 查询）。
	cacheTTL time.Duration
	mu       sync.Mutex
	cache    map[int]cacheEntry
}

type cacheEntry struct {
	ov *Overview
	at time.Time
}

func NewService(logs logquery.Store, subnets SubnetSource, bindings BindingSource, lights Lights) *Service {
	return &Service{logs: logs, subnets: subnets, bindings: bindings, lights: lights, now: time.Now,
		cacheTTL: 5 * time.Second, cache: map[int]cacheEntry{}}
}

// SetCacheTTL 覆盖快照缓存 TTL（≤0 关闭缓存；env：IPAM_DASHBOARD_CACHE_TTL）。
func (s *Service) SetCacheTTL(d time.Duration) {
	s.mu.Lock()
	s.cacheTTL = d
	s.cache = map[int]cacheEntry{}
	s.mu.Unlock()
}

func (s *Service) NowUTC() time.Time { return s.now().UTC().Truncate(time.Second) }

// PoolUtil 池利用率行。
type PoolUtil struct {
	SubnetID string
	Name     string
	CIDR     string
	PoolIdx  int
	Used     int64
	Capacity int64
	Pct      float64
}

// Overview 聚合结果（与契约 DashboardOverview 对齐）。
type Overview struct {
	TS               time.Time
	TodayStart       time.Time
	OnlineNow        int64
	OnlineTrend      []logquery.QpsPoint
	NewTerminals     *int64
	OfflineTerminals *int64
	Lights           map[string]Light
	Qps5m            *float64
	HitRatePct       *int64 // unbound 命中语义日志落地前恒 nil
	BlockedToday     *int64
	PoolTop          []PoolUtil
	CoherencePct     *int64
}

// Overview 单请求聚合（≤1s 预算）：4 条 CH 查询并发执行 + 内存侧池内计数，
// 结果按 cacheTTL 缓存，避免多客户端轮询放大 ClickHouse 压力。
func (s *Service) Overview(ctx context.Context, poolTopN int) (*Overview, error) {
	now := s.NowUTC()
	if ov := s.cached(poolTopN, now); ov != nil {
		return ov, nil
	}
	// 串行化刷新：并发请求排队等首个快照，避免缓存击穿。
	s.mu.Lock()
	defer s.mu.Unlock()
	now = s.NowUTC()
	if ov := s.cachedLocked(poolTopN, now); ov != nil {
		return ov, nil
	}
	ov := s.build(ctx, poolTopN, now)
	if s.cacheTTL > 0 {
		s.cache[poolTopN] = cacheEntry{ov: ov, at: now}
	}
	return ov, nil
}

func (s *Service) cached(poolTopN int, now time.Time) *Overview {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cachedLocked(poolTopN, now)
}

func (s *Service) cachedLocked(poolTopN int, now time.Time) *Overview {
	e, ok := s.cache[poolTopN]
	if !ok || s.cacheTTL <= 0 || now.Sub(e.at) > s.cacheTTL {
		return nil
	}
	return e.ov
}

func (s *Service) build(ctx context.Context, poolTopN int, now time.Time) *Overview {
	today := now.Truncate(24 * time.Hour)
	ov := &Overview{TS: now, TodayStart: today,
		Lights: map[string]Light{
			"postgres":   s.lights.of(ctx, s.lights.Postgres),
			"clickhouse": s.lights.of(ctx, s.lights.ClickHouse),
			"kea":        s.lights.of(ctx, s.lights.Kea),
			"unbound":    s.lights.of(ctx, s.lights.Unbound),
		}}

	// 四个数据源彼此独立 → 并发取数；任一失败按「该指标降级为空」处理，不拖垮整页。
	var (
		ips            []string
		trend          []logquery.QpsPoint
		fresh, offline *int64
		qps            *float64
		blockedToday   *int64
	)
	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		if v, err := s.logs.DistinctClientIPs(ctx,
			logquery.LogFilter{From: today, To: now, Type: "dhcp"}, logquery.OrgScope{}); err == nil {
			ips = v
		}
	}()
	go func() {
		defer wg.Done()
		if v, err := s.logs.HourlyActive(ctx, now.Add(-24*time.Hour), now); err == nil {
			trend = v
		}
	}()
	go func() {
		defer wg.Done()
		fresh, offline = s.terminalFlow(ctx, now, today)
	}()
	go func() {
		defer wg.Done()
		qps, blockedToday = s.dnsMetrics(ctx, now, today)
	}()
	wg.Wait()

	ov.OnlineNow = int64(len(ips))
	ov.OnlineTrend = fillHourly(trend, now)
	ov.NewTerminals, ov.OfflineTerminals = fresh, offline
	ov.Qps5m, ov.BlockedToday = qps, blockedToday
	ov.PoolTop = s.poolTop(ctx, poolTopN, ips)
	s.coherenceRate(ctx, ov)
	return ov
}

// terminalFlow 新增/离线终端（近 7 天 MAC 活动窗口推导）。
func (s *Service) terminalFlow(ctx context.Context, now, today time.Time) (*int64, *int64) {
	act, err := s.logs.MacActivity(ctx, today.Add(-7*24*time.Hour), now)
	if err != nil || len(act) == 0 {
		return nil, nil
	}
	var fresh, offline int64
	for _, w := range act {
		first := w[0].UTC()
		last := w[1].UTC()
		if !first.Before(today) {
			fresh++
		}
		if last.Before(today) && !last.Before(today.Add(-24*time.Hour)) {
			offline++
		}
	}
	return &fresh, &offline
}

// dnsMetrics qps5m 与今日拦截量。
func (s *Service) dnsMetrics(ctx context.Context, now, today time.Time) (*float64, *int64) {
	var qps *float64
	points, err := s.logs.Qps(ctx, logquery.QpsQuery{
		From: now.Add(-5 * time.Minute), To: now, Type: "dns", IntervalSec: 300}, logquery.OrgScope{})
	if err == nil {
		total := int64(0)
		for _, p := range points {
			total += int64(p.Count)
		}
		q := float64(total) / 300
		qps = &q
	}
	var blocked *int64
	page, err := s.logs.Query(ctx, logquery.LogFilter{
		From: today, To: now, Action: "blocked", PageSize: 1}, logquery.OrgScope{})
	if err == nil {
		b := int64(page.Total)
		blocked = &b
	}
	return qps, blocked
}

// poolTop 池利用率 TopN（used=今日区间 distinct client_ip）。
// 今日活动 IP 集合只取一次，按池区间在内存侧二分计数——替代以往「每池一次 CH 查询」。
func (s *Service) poolTop(ctx context.Context, n int, ips []string) []PoolUtil {
	if n <= 0 {
		n = 5
	}
	if s.subnets == nil {
		return []PoolUtil{}
	}
	addrs := make([]netip.Addr, 0, len(ips))
	for _, ip := range ips {
		if a, err := netip.ParseAddr(ip); err == nil {
			addrs = append(addrs, a.Unmap())
		}
	}
	sort.Slice(addrs, func(i, j int) bool { return addrs[i].Compare(addrs[j]) < 0 })

	out := []PoolUtil{}
	for _, sn := range s.subnets(ctx) {
		for i, p := range sn.Pools {
			rng, cap, ok := poolRangeCapacity(p.StartAddr, p.EndAddr)
			if !ok {
				continue
			}
			lo, errL := netip.ParseAddr(rng.Lo)
			hi, errH := netip.ParseAddr(rng.Hi)
			if errL != nil || errH != nil {
				continue
			}
			used := int64(countAddrInRange(addrs, lo.Unmap(), hi.Unmap()))
			pct := 0.0
			if cap > 0 {
				pct = float64(used) / float64(cap) * 100
			}
			out = append(out, PoolUtil{
				SubnetID: sn.ID, Name: sn.Name, CIDR: sn.CIDR,
				PoolIdx: i, Used: used, Capacity: cap, Pct: pct,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Used > out[j].Used })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// countAddrInRange 已排序地址集合中落在 [lo,hi] 闭区间的个数。
func countAddrInRange(addrs []netip.Addr, lo, hi netip.Addr) int {
	i := sort.Search(len(addrs), func(i int) bool { return addrs[i].Compare(lo) >= 0 })
	j := sort.Search(len(addrs), func(i int) bool { return addrs[i].Compare(hi) > 0 })
	if j < i {
		return 0
	}
	return j - i
}

// coherenceRate active/(active+conflict)。
func (s *Service) coherenceRate(ctx context.Context, ov *Overview) {
	if s.bindings == nil {
		return
	}
	bs, err := s.bindings(ctx)
	if err != nil || len(bs) == 0 {
		return
	}
	var active, conflict int64
	for _, b := range bs {
		switch b.State {
		case "active":
			active++
		case "conflict":
			conflict++
		}
	}
	denom := active + conflict
	if denom == 0 {
		return
	}
	pct := active * 100 / denom
	ov.CoherencePct = &pct
}

// fillHourly 24 槽补零。
func fillHourly(pts []logquery.QpsPoint, now time.Time) []logquery.QpsPoint {
	m := map[int64]int{}
	for _, p := range pts {
		m[p.TS.UTC().Unix()] = p.Count
	}
	last := now.Truncate(time.Hour).UTC()
	out := make([]logquery.QpsPoint, 0, 24)
	for i := 23; i >= 0; i-- {
		h := last.Add(-time.Duration(i) * time.Hour)
		out = append(out, logquery.QpsPoint{TS: h, Count: m[h.Unix()]})
	}
	return out
}

// poolRangeCapacity 池闭区间容量（大段按 int64 上限饱和）。
func poolRangeCapacity(start, end string) (logquery.AddrRange, int64, bool) {
	lo, err1 := netip.ParseAddr(start)
	hi, err2 := netip.ParseAddr(end)
	if err1 != nil || err2 != nil || lo.Compare(hi) > 0 {
		return logquery.AddrRange{}, 0, false
	}
	diff := new(big.Int).Sub(addrBig(hi), addrBig(lo))
	var capacity int64
	if diff.IsInt64() {
		capacity = diff.Int64() + 1
	} else {
		capacity = int64(^uint64(0) >> 1)
	}
	return logquery.AddrRange{Lo: start, Hi: end}, capacity, true
}

func addrBig(a netip.Addr) *big.Int {
	b16 := a.Unmap().As16()
	return new(big.Int).SetBytes(b16[:])
}
