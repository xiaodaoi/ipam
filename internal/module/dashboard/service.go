// Package dashboard 仪表盘聚合域（M4-004，F-01、§13.4 一级菜单）。
// 单端点聚合多源指标：logquery.Store 日志口径 + 子网/联动绑定来源 + 进程探针健康灯。
package dashboard

import (
	"context"
	"math/big"
	"net/netip"
	"sort"
	"strings"
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

// TerminalStatsSource 终端登记概览（未登记/忽略/已登记），与「未登记终端」页同口径（M6-048）。
type TerminalStatsSource func(ctx context.Context) (unregistered, ignored, assets int, err error)

// LedgerSource 地址台账数据源（租约/预留/子网），用于在线终端与池利用率口径。
// 与 DHCP 子网页共用同一数据源，保证「在线租约」数字一致（M6-043）。
type LedgerSource func(ctx context.Context) ipam.LedgerSource

// Service 聚合编排。
type Service struct {
	logs     logquery.Store
	subnets  SubnetSource
	bindings BindingSource
	ledger   LedgerSource
	terminal TerminalStatsSource
	lights   Lights
	now      func() time.Time // 测试注入

	// orgName 组织（部门）名解析；nil 时空值（组织未接入/PoC）。M6-020。
	orgName func(id string) string

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

// SetOrgNameResolver 注入组织（部门）名解析器（M6-020）；nil 时地址池行 orgName 为空。
func (s *Service) SetOrgNameResolver(fn func(id string) string) {
	s.mu.Lock()
	s.orgName = fn
	s.cache = map[int]cacheEntry{}
	s.mu.Unlock()
}

// SetLedgerSource 注入地址台账数据源（M6-043）：在线终端数与池已用改为租约口径，
// 与 DHCP 子网页保持一致。未注入时回退日志活跃 IP（兼容 PoC/单测）。
func (s *Service) SetLedgerSource(fn LedgerSource) {
	s.mu.Lock()
	s.ledger = fn
	s.cache = map[int]cacheEntry{}
	s.mu.Unlock()
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
	OrgID    string
	OrgName  string
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
	OnlineV4         int64 // 当前在线租约 IPv4 地址数（与 DHCP 子网页 family=4 一致）
	OnlineV6         int64 // 当前在线租约 IPv6 地址数（与 DHCP 子网页 family=6 一致）
	OnlineTrend      []logquery.QpsPoint
	NewTerminals     *int64
	OfflineTerminals *int64
	Lights           map[string]Light
	Qps5m            *float64
	HitRatePct       *int64 // unbound 命中语义日志落地前恒 nil
	BlockedToday     *int64
	PoolTop          []PoolUtil
	CoherencePct     *int64
	// 终端登记概览（M6-048）：未登记/忽略/已登记，供仪表盘 KPI 与登记率。
	UnregisteredTerminals *int64
	IgnoredTerminals      *int64
	AssetTotal            *int64
}

// SetTerminalStats 注入终端登记概览数据源（资产域不可用时保持 nil → 前端显示 —）。
func (s *Service) SetTerminalStats(fn TerminalStatsSource) { s.terminal = fn }

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

	// 台账只取一次：在线租约与「近 24h 无租约」判定共用同一快照（M6-045）。
	// 放在并发取数之前，保证两处口径来自同一时点。
	var lease leaseSnapshot
	leaseOK := false
	if s.ledger != nil {
		if src := s.ledger(ctx); len(src.Subnets) > 0 {
			lease = onlineLeaseSnapshot(src, now)
			leaseOK = true
		}
	}

	// 四个数据源彼此独立 → 并发取数；任一失败按「该指标降级为空」处理，不拖垮整页。
	var (
		ips                   []string
		trend                 []logquery.QpsPoint
		fresh, offline        *int64
		qps                   *float64
		blockedToday          *int64
		unregistered, ignored *int64
		assetTotal            *int64
	)
	var wg sync.WaitGroup
	wg.Add(5)
	go func() {
		defer wg.Done()
		// 在线终端 = 当前有效租约（与地址台账/DHCP 子网页同口径）；台账不可用时回退日志活跃 IP。
		if leaseOK {
			ips = lease.addrs
			return
		}
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
		fresh, offline = s.terminalFlow(ctx, now, today, lease.macs, leaseOK)
	}()
	go func() {
		defer wg.Done()
		qps, blockedToday = s.dnsMetrics(ctx, now, today)
	}()
	go func() {
		defer wg.Done()
		if s.terminal == nil {
			return
		}
		u, ig, total, err := s.terminal(ctx)
		if err != nil {
			return
		}
		uv, igv, tv := int64(u), int64(ig), int64(total)
		unregistered, ignored, assetTotal = &uv, &igv, &tv
	}()
	wg.Wait()

	ov.OnlineNow = int64(len(ips))
	for _, ip := range ips {
		if a, err := netip.ParseAddr(ip); err == nil {
			if a.Is4() {
				ov.OnlineV4++
			} else {
				ov.OnlineV6++
			}
		}
	}
	ov.OnlineTrend = fillHourly(trend, now)
	ov.NewTerminals, ov.OfflineTerminals = fresh, offline
	ov.Qps5m, ov.BlockedToday = qps, blockedToday
	ov.PoolTop = s.poolTop(ctx, poolTopN, ips)
	ov.UnregisteredTerminals, ov.IgnoredTerminals, ov.AssetTotal = unregistered, ignored, assetTotal
	s.coherenceRate(ctx, ov)
	return ov
}

// terminalFlow 新增/离线终端（近 7 天 MAC 活动窗口推导，M6-045）。
// 离线口径与在线租约对齐：近 24h 有 DHCP 活动且当前无有效租约（按 MAC 比对），
// 不再用“今日 0 点后未见”这种日切窗口，避免凌晨出现“在线很多、离线也很多”。
// 台账不可用时回退旧口径（今日未见）。
func (s *Service) terminalFlow(ctx context.Context, now, today time.Time, online map[string]struct{}, leaseOK bool) (*int64, *int64) {
	act, err := s.logs.MacActivity(ctx, today.Add(-7*24*time.Hour), now)
	if err != nil || len(act) == 0 {
		return nil, nil
	}
	floor := now.Add(-24 * time.Hour)
	var fresh, offline int64
	for mac, w := range act {
		first := w[0].UTC()
		last := w[1].UTC()
		if !first.Before(today) {
			fresh++
		}
		if last.Before(floor) {
			continue
		}
		if leaseOK {
			if _, ok := online[normalizeMAC(mac)]; !ok {
				offline++
			}
		} else if last.Before(today) {
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

// leaseSnapshot 在线租约快照：地址集合 + 归一化 MAC 集合。
type leaseSnapshot struct {
	addrs []string
	macs  map[string]struct{}
}

// onlineLeaseSnapshot 从台账数据源提取当前在线租约（M6-044/M6-045）。
// 口径与平台「在线 = 租约存在」一致（§13.4）：
//   - 只认 active 绑定；grace 在台账里是「过期/宽限」单列状态，不计在线；
//   - 落在已配置子网内，剔除静态/保留与未指定地址（0.0.0.0 / ::）；
//   - cltt/valid_lft 可判定时校验租期未过期，避免绑定状态滞后造成假在线。
func onlineLeaseSnapshot(src ipam.LedgerSource, now time.Time) leaseSnapshot {
	prefixes := make([]netip.Prefix, 0, len(src.Subnets))
	for _, sn := range src.Subnets {
		if p, err := netip.ParsePrefix(sn.CIDR); err == nil {
			prefixes = append(prefixes, p)
		}
	}
	reserved := map[string]struct{}{}
	for _, r := range src.Reservations {
		if r.IPv4 == "" {
			continue
		}
		if a, err := netip.ParseAddr(r.IPv4); err == nil {
			reserved[a.Unmap().String()] = struct{}{}
		}
	}
	seen := map[string]struct{}{}
	add := func(raw string) {
		if raw == "" {
			return
		}
		a, err := netip.ParseAddr(raw)
		if err != nil {
			return
		}
		a = a.Unmap()
		if !a.IsValid() || a.IsUnspecified() {
			return
		}
		if _, ok := reserved[a.String()]; ok {
			return
		}
		for _, p := range prefixes {
			if p.Contains(a) {
				seen[a.String()] = struct{}{}
				return
			}
		}
	}
	macs := map[string]struct{}{}
	nowUnix := now.Unix()
	for _, b := range src.Bindings {
		if b.State != "" && b.State != "active" {
			continue
		}
		if b.Cltt > 0 && b.ValidLft > 0 && b.Cltt+int64(b.ValidLft) <= nowUnix {
			continue // 租期已过：状态未及时翻转时仍按离线处理
		}
		before := len(seen)
		add(b.IPv4)
		add(b.IPv6)
		if len(seen) > before && b.MAC != "" {
			macs[normalizeMAC(b.MAC)] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for ip := range seen {
		out = append(out, ip)
	}
	sort.Strings(out)
	return leaseSnapshot{addrs: out, macs: macs}
}

// normalizeMAC 统一 MAC/DUID 文本（小写、去分隔符）：日志 client_mac 为无分隔小写，
// 台账可能带冒号，双栈 v6 还可能用 DUID 十六进制，需归一后才能比对。
func normalizeMAC(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, ":", "")
	s = strings.ReplaceAll(s, "-", "")
	return s
}

// poolTop 池利用率 TopN（used=当前池内在线租约地址数）。
// 与 DHCP 子网页共用租约口径；IPv6 PD 池不是地址池，跳过。
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
		orgName := ""
		if s.orgName != nil && sn.OrgID != "" {
			orgName = s.orgName(sn.OrgID)
		}
		for i, p := range sn.Pools {
			if p.Kind == "pd" {
				continue // PD 是委派前缀，不是可用地址池
			}
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
				OrgID: sn.OrgID, OrgName: orgName,
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
