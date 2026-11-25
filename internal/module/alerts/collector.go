package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xiaodaoi/ipam/internal/module/logquery"
)

// DNSSource 风险域名聚合来源（由 ClickHouse 实现；内存模式不启用）。
type DNSSource interface {
	BlockedEvents(ctx context.Context, since time.Time) ([]logquery.BlockedGroup, error)
}

// DomainAttributor 把被拦截域名归属到外部情报源（支持后缀匹配）。
// keys/names 为逗号分隔的稳定 key 与显示名；未命中返回空串。
type DomainAttributor interface {
	AttributeDomain(ctx context.Context, domain string) (keys, names string)
}

// DiskUsage 磁盘水位快照。
type DiskUsage struct {
	Mount      string
	UsedPct    float64
	TotalBytes uint64
	UsedBytes  uint64
}

// SourceIssue 情报源同步失败。
type SourceIssue struct {
	Key  string
	Name string
	Err  string
}

// UpstreamIssue 上游探活失败。
type UpstreamIssue struct {
	ID    string
	Name  string
	Addrs string
	Fails int
}

// SystemChecks 系统故障探针集合；nil 表示该能力未配置（不产生报警，也不参与自动恢复）。
type SystemChecks struct {
	Postgres          func(ctx context.Context) error
	ClickHouse        func(ctx context.Context) error
	Kea               func(ctx context.Context) error
	Unbound           func(ctx context.Context) error
	Disks             func() []DiskUsage
	LogExportDegraded func() bool
	IntelSources      func(ctx context.Context) []SourceIssue
	Upstreams         func(ctx context.Context) []UpstreamIssue
}

// Collector 报警采集器：DNS 风险访问 + 系统故障。
type Collector struct {
	svc      *Service
	dns      DNSSource
	attr     DomainAttributor
	notifier Notifier
	checks   SystemChecks
	logf     func(format string, args ...any)
	now      func() time.Time
	fails    map[string]int
	purged   time.Time

	capMu    sync.Mutex
	capHour  string
	capCount map[string]int
}

// ServiceFailThreshold 连续失败次数达到该值才产生系统报警（避免网络抖动误报）。
const ServiceFailThreshold = 2

// DefaultGlobalNotifyLimit 全局每小时通知上限兜底（可由全局策略覆盖）。
const DefaultGlobalNotifyLimit = 30

// NewCollector 构造。
func NewCollector(svc *Service, checks SystemChecks) *Collector {
	return &Collector{
		svc:      svc,
		checks:   checks,
		logf:     log.Printf,
		now:      func() time.Time { return time.Now().UTC() },
		fails:    map[string]int{},
		capCount: map[string]int{},
	}
}

// SetDNSSource 接入 ClickHouse 风险访问聚合。
func (c *Collector) SetDNSSource(src DNSSource) { c.dns = src }

// SetAttributor 接入情报来源归属（源 key/显示名）。
func (c *Collector) SetAttributor(a DomainAttributor) { c.attr = a }

// SetNotifier 接入外部通知通道（nil=仅站内铃铛）。
func (c *Collector) SetNotifier(n Notifier) { c.notifier = n }

// SetLogger 注入日志函数（单测静默）。
func (c *Collector) SetLogger(f func(format string, args ...any)) {
	if f != nil {
		c.logf = f
	}
}

// SetNow 注入时钟（单测）。
func (c *Collector) SetNow(f func() time.Time) {
	if f != nil {
		c.now = f
	}
}

// Run 每 60s 采集一次；每日清理一次过期报警。
func (c *Collector) Run(ctx context.Context) {
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	c.CollectOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.CollectOnce(ctx)
		}
	}
}

// CollectOnce 执行一轮 DNS + 系统采集与清理。
func (c *Collector) CollectOnce(ctx context.Context) {
	if n, err := c.CollectDNS(ctx); err != nil {
		c.logf("[alerts] DNS 风险访问采集失败：%v", err)
	} else if n > 0 {
		c.logf("[alerts] DNS 风险访问：合并 %d 组", n)
	}
	if n, err := c.svc.AutoResolveDNS(ctx); err == nil && n > 0 {
		c.logf("[alerts] DNS 风险访问自动恢复 %d 条", n)
	}
	if n, err := c.CollectSystem(ctx); err != nil {
		c.logf("[alerts] 系统故障采集失败：%v", err)
	} else if n > 0 {
		c.logf("[alerts] 系统故障：上报 %d 项", n)
	}
	if c.purged.IsZero() || c.now().Sub(c.purged) >= 24*time.Hour {
		if n, err := c.svc.Purge(ctx, c.now().Add(-Retention)); err == nil {
			c.purged = c.now()
			if n > 0 {
				c.logf("[alerts] 清理过期报警 %d 条", n)
			}
		}
	}
}

type dnsCandidate struct {
	ev       Event
	domains  map[string]int
	severity string
}

func splitKeys(s string) []string {
	out := make([]string, 0, 2)
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func intelDisplay(names string) string {
	parts := splitKeys(names)
	if len(parts) == 0 {
		return "自定义封禁/内置规则"
	}
	return strings.Join(parts, " / ")
}

func primaryKey(keys string) string {
	if ks := splitKeys(keys); len(ks) > 0 {
		return ks[0]
	}
	return "custom"
}

func groupKeyFor(groupBy, sourceKey, client, domain string) string {
	switch groupBy {
	case GroupByClient:
		return fmt.Sprintf("dns|client|%s|%s", sourceKey, client)
	case GroupBySource:
		return fmt.Sprintf("dns|source|%s|%s", sourceKey, client)
	case GroupByNone:
		return fmt.Sprintf("dns|rollup|%s", sourceKey)
	default:
		return fmt.Sprintf("dns|domain|%s|%s|%s", sourceKey, client, domain)
	}
}

// CollectDNS 回扫最近 2 小时被拦截的 DNS 访问，按策略分级/聚合/限流。
//
// 每次回扫拿到的是桶内累计值，Upsert 使用 GREATEST 合并，因此重复扫描不会重复计数。
func (c *Collector) CollectDNS(ctx context.Context) (int, error) {
	if c.dns == nil {
		return 0, nil
	}
	since := c.now().Add(-2 * time.Hour)
	groups, err := c.dns.BlockedEvents(ctx, since)
	if err != nil {
		return 0, err
	}
	global := c.svc.ResolvePolicy(ctx, PolicyInput{Source: SourceDNS, Kind: KindRiskURL})
	globalLimit := DefaultGlobalNotifyLimit
	if global.MaxPerHour > 0 {
		globalLimit = global.MaxPerHour
	}

	rollups := map[string]*dnsCandidate{}
	candidates := make([]*dnsCandidate, 0, len(groups))
	var firstErr error
	for _, g := range groups {
		if g.ClientIP == "" || g.Domain == "" || g.Count <= 0 {
			continue
		}
		// 通配规则名（*.corp.example）不是真实查询域名，不能进入报警（M6-040 防御）。
		if strings.Contains(g.Domain, "*") {
			continue
		}
		bucket := g.Bucket.UTC()
		if bucket.IsZero() {
			bucket = g.LastSeen.UTC().Truncate(time.Hour)
		}
		domain := strings.ToLower(g.Domain)
		intelKey, intelName := "", ""
		if c.attr != nil {
			intelKey, intelName = c.attr.AttributeDomain(ctx, domain)
		}
		in := PolicyInput{
			Source: SourceDNS, Kind: KindRiskURL, Component: "dns",
			IntelSources: splitKeys(intelKey), ClientIP: g.ClientIP, Domain: domain,
		}
		eff := c.svc.ResolvePolicy(ctx, in)
		// off=仅保留日志，不生成报警；被静音时 mode 可能仍是 notify，需要落库归档。
		if eff.NotifyMode == NotifyModeOff && eff.SuppressedBy == "" {
			continue
		}
		sourceKey := primaryKey(intelKey)
		groupKey := groupKeyFor(eff.GroupBy, sourceKey, g.ClientIP, domain)
		ev := Event{
			DedupKey:        groupKey,
			Fingerprint:     groupKey,
			Source:          SourceDNS,
			Kind:            KindRiskURL,
			Severity:        eff.Severity,
			Title:           fmt.Sprintf("风险域名访问：%s", g.Domain),
			Message:         fmt.Sprintf("客户端 %s 在 %s 这一小时内访问被拦截域名 %d 次", g.ClientIP, bucket.Format("2006-01-02 15:00"), g.Count),
			ClientIP:        g.ClientIP,
			Domain:          domain,
			Component:       "dns",
			IntelSource:     intelKey,
			IntelSourceName: intelName,
			AlertClass:      eff.AlertClass,
			Notify:          eff.Notify,
			PolicyID:        eff.ID,
			GroupKey:        groupKey,
			Count:           g.Count,
			Detail:          g.Category,
			WindowStart:     bucket,
			SuppressedBy:    eff.SuppressedBy,
			SilenceID:       eff.SilenceID,
			Decision:        DecisionOf(eff),
			RepeatAfterSec:  eff.RepeatAfterSec,
			ResolveAfterSec: eff.ResolveAfterSec,
			MaxPerHour:      eff.MaxPerHour,
			FirstSeen:       g.FirstSeen.UTC(),
			LastSeen:        g.LastSeen.UTC(),
		}
		rc := rollups[groupKey]
		if rc == nil {
			ev.Count = 0
			rc = &dnsCandidate{ev: ev, domains: map[string]int{}, severity: eff.Severity}
			rollups[groupKey] = rc
			candidates = append(candidates, rc)
		}
		if eff.GroupBy == GroupByDomain {
			rc.domains[domain] = g.Count
			rc.ev.Count = g.Count
			continue
		}
		// 聚合：计数累加，窗口取并集，严重级别取最高。
		rc.ev.Count += g.Count
		if ev.FirstSeen.Before(rc.ev.FirstSeen) || rc.ev.FirstSeen.IsZero() {
			rc.ev.FirstSeen = ev.FirstSeen
		}
		if ev.LastSeen.After(rc.ev.LastSeen) {
			rc.ev.LastSeen = ev.LastSeen
		}
		if rc.ev.WindowStart.IsZero() || ev.WindowStart.Before(rc.ev.WindowStart) {
			rc.ev.WindowStart = ev.WindowStart
		}
		rc.domains[domain] += g.Count
		if severityRank(ev.Severity) > severityRank(rc.severity) {
			rc.severity = ev.Severity
			rc.ev.Severity = ev.Severity
		}
		if ev.Notify {
			rc.ev.Notify = true
		}
		rc.ev.MaxPerHour = maxInt(rc.ev.MaxPerHour, ev.MaxPerHour)
		rc.ev.IntelSource = mergeCSV(rc.ev.IntelSource, ev.IntelSource)
		rc.ev.IntelSourceName = mergeCSV(rc.ev.IntelSourceName, ev.IntelSourceName)
		rc.ev.SuppressedBy = ev.SuppressedBy
		rc.ev.SilenceID = ev.SilenceID
	}

	// 生成 rollup 标题/详情，排序（critical 优先、次数降序），再做限流。
	out := make([]Event, 0, len(candidates))
	for _, rc := range candidates {
		if rc.ev.GroupKey != "" && !strings.Contains(rc.ev.GroupKey, "|domain|") {
			display := intelDisplay(rc.ev.IntelSourceName)
			rc.ev.Title = fmt.Sprintf("已拦截 %d 次风险访问（%s）", rc.ev.Count, display)
			rc.ev.Message = fmt.Sprintf("客户端 %s 在 %s 起的窗口内命中 %d 次，来源：%s",
				rc.ev.ClientIP, rc.ev.WindowStart.Format("2006-01-02 15:00"), rc.ev.Count, display)
			rc.ev.Detail = fmt.Sprintf("%s；Top 域名：%s", rc.ev.Detail, topDomains(rc.domains, 5))
			rc.ev.Domain = ""
		}
		out = append(out, rc.ev)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := severityRank(out[i].Severity), severityRank(out[j].Severity)
		if ri != rj {
			return ri > rj
		}
		return out[i].Count > out[j].Count
	})

	reported := 0
	for _, ev := range out {
		sourceKey := primaryKey(ev.IntelSource)
		if ev.Source == SourceDNS && ev.Notify &&
			(c.capExceeded(sourceKey, ev.MaxPerHour) || c.capExceeded("__global__", globalLimit)) {
			ev.Notify = false
			ev.SuppressedBy = "限流：超过每小时通知上限，已合并为归档"
			ev.Decision = decisionWithCap(ev.Decision)
		}
		a, err := c.svc.Report(ctx, ev)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		reported++
		// 仅在实际触发通知时消耗预算：重复回扫合并不占用额度，避免把上限吃光。
		if a.JustNotified {
			c.consumeBudget(sourceKey, "__global__")
			if c.notifier != nil {
				c.notifier.Notify(ctx, a)
			}
		}
	}
	return reported, firstErr
}

func decisionWithCap(raw string) string {
	d := Decision{}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &d)
	}
	d.Capped = true
	d.Notify = false
	d.Final = NotifyModeDigest
	out, _ := json.Marshal(d)
	return string(out)
}

func mergeCSV(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" || strings.Contains(a, b) {
		return a
	}
	return a + "," + b
}

func topDomains(m map[string]int, n int) string {
	type kv struct {
		k string
		v int
	}
	list := make([]kv, 0, len(m))
	for k, v := range m {
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].v != list[j].v {
			return list[i].v > list[j].v
		}
		return list[i].k < list[j].k
	})
	if len(list) > n {
		list = list[:n]
	}
	parts := make([]string, 0, len(list))
	for _, x := range list {
		parts = append(parts, fmt.Sprintf("%s(%d)", x.k, x.v))
	}
	return strings.Join(parts, ", ")
}

// allow 每小时通知预算；key 独立计数。
// capExceeded 只读检查每小时通知预算；实际消耗在真正触发通知后调用 consumeBudget。
func (c *Collector) capExceeded(key string, limit int) bool {
	if limit <= 0 {
		return false
	}
	hour := c.now().Format("2006010215")
	c.capMu.Lock()
	defer c.capMu.Unlock()
	c.rotateCap(hour)
	return c.capCount[key] >= limit
}

// consumeBudget 记录一次真实通知（按来源与全局分别计数）。
func (c *Collector) consumeBudget(keys ...string) {
	hour := c.now().Format("2006010215")
	c.capMu.Lock()
	defer c.capMu.Unlock()
	c.rotateCap(hour)
	for _, k := range keys {
		if k != "" {
			c.capCount[k]++
		}
	}
}

func (c *Collector) rotateCap(hour string) {
	if c.capHour != hour {
		c.capHour, c.capCount = hour, map[string]int{}
	}
}

// CollectSystem 采集系统故障，并把已恢复的报警自动置为 resolved。
func (c *Collector) CollectSystem(ctx context.Context) (int, error) {
	managed := make([]string, 0, 10)
	faulty := make([]string, 0, 16)
	reported := 0
	var firstErr error
	dnsProbeFailed := false

	report := func(ev Event) {
		faulty = append(faulty, ev.DedupKey)
		presetSuppressed, presetDecision := ev.SuppressedBy, ev.Decision
		eff := c.svc.ResolvePolicy(ctx, PolicyInput{
			Source: ev.Source, Kind: ev.Kind, Component: ev.Component,
		})
		ev.Severity = eff.Severity
		ev.Notify = eff.Notify
		ev.AlertClass = eff.AlertClass
		ev.PolicyID = eff.ID
		ev.GroupKey = ev.DedupKey
		ev.SuppressedBy = eff.SuppressedBy
		ev.SilenceID = eff.SilenceID
		ev.Decision = DecisionOf(eff)
		// 保留采集阶段的内置抑制（如 DNS 不可用抑制上游探活）。
		if presetSuppressed != "" {
			ev.Notify = false
			ev.SuppressedBy = presetSuppressed
			ev.Decision = presetDecision
		}
		ev.RepeatAfterSec = eff.RepeatAfterSec
		ev.ResolveAfterSec = eff.ResolveAfterSec
		ev.WindowStart = c.now().Truncate(time.Hour)
		a, err := c.svc.Report(ctx, ev)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return
		}
		reported++
		if a.JustNotified && c.notifier != nil {
			c.notifier.Notify(ctx, a)
		}
	}

	// 服务探针：连续失败达到阈值才报警；失败但未达阈值也计入 faulty，避免被误判为已恢复。
	probe := func(kind, component, title, message string, fn func(context.Context) error) {
		if fn == nil {
			return
		}
		managed = append(managed, kind)
		key := "system|" + kind + "|" + component
		if err := fn(ctx); err != nil {
			c.fails[key]++
			faulty = append(faulty, key)
			if kind == KindDNSDown {
				dnsProbeFailed = true
			}
			if c.fails[key] < ServiceFailThreshold {
				return
			}
			report(Event{
				DedupKey: key, Source: SourceSystem, Kind: kind, Severity: SeverityCritical,
				Title: title, Message: message, Component: component,
				Count: c.fails[key], Detail: err.Error(), LastSeen: c.now(),
			})
			return
		}
		c.fails[key] = 0
	}
	probe(KindDBDown, "database", "数据库连接异常", "数据库健康探针连续失败，业务读写可能受影响", c.checks.Postgres)
	probe(KindLogStoreDown, "logstore", "日志存储不可用", "日志存储健康探针连续失败，日志检索与写入可能受影响", c.checks.ClickHouse)
	probe(KindDHCPDown, "dhcp", "DHCP 服务不可用", "DHCP 服务控制接口连续失败，地址分配可能受影响", c.checks.Kea)
	probe(KindDNSDown, "dns", "DNS 服务不可用", "DNS 服务控制套接字不可达，解析与策略下发可能受影响", c.checks.Unbound)

	if c.checks.Disks != nil {
		managed = append(managed, KindDiskPressure)
		for _, d := range c.checks.Disks() {
			if d.UsedPct < 90 {
				continue
			}
			key := "system|" + KindDiskPressure + "|" + d.Mount
			report(Event{
				DedupKey: key, Source: SourceSystem, Kind: KindDiskPressure, Severity: SeverityWarning,
				Title:     fmt.Sprintf("磁盘空间不足：%s", d.Mount),
				Message:   fmt.Sprintf("磁盘 %s 使用率 %.1f%%（阈值 90%%），请及时清理或扩容", d.Mount, d.UsedPct),
				Component: "host", Count: 1,
				Detail:   fmt.Sprintf("total=%d used=%d", d.TotalBytes, d.UsedBytes),
				LastSeen: c.now(),
			})
		}
	}

	if c.checks.LogExportDegraded != nil {
		managed = append(managed, KindLogExportDegraded)
		if c.checks.LogExportDegraded() {
			report(Event{
				DedupKey: "system|" + KindLogExportDegraded + "|log-export",
				Source:   SourceSystem, Kind: KindLogExportDegraded, Severity: SeverityWarning,
				Title: "日志入库与外发已降级", Message: "磁盘水位过高：内部日志按 1/N 抽样写入（保留 INFO），外发仅异常；统计数值为抽样结果",
				Component: "log-export", Count: 1, LastSeen: c.now(),
			})
		}
	}

	if c.checks.IntelSources != nil {
		managed = append(managed, KindIntelSyncFailed)
		for _, s := range c.checks.IntelSources(ctx) {
			report(Event{
				DedupKey: "system|" + KindIntelSyncFailed + "|" + s.Key,
				Source:   SourceSystem, Kind: KindIntelSyncFailed, Severity: SeverityWarning,
				Title:     fmt.Sprintf("情报源同步失败：%s", s.Name),
				Message:   fmt.Sprintf("情报源「%s」最近一次同步失败，已保留上一版数据", s.Name),
				Component: "intel", Count: 1, Detail: truncate(s.Err, 500), LastSeen: c.now(),
			})
		}
	}

	if c.checks.Upstreams != nil {
		managed = append(managed, KindUpstreamProbeFailed)
		for _, u := range c.checks.Upstreams(ctx) {
			ev := Event{
				DedupKey: "system|" + KindUpstreamProbeFailed + "|" + u.ID,
				Source:   SourceSystem, Kind: KindUpstreamProbeFailed, Severity: SeverityWarning,
				Title: fmt.Sprintf("上游 DNS 不可达：%s", u.Name),
				Message: fmt.Sprintf("上游「%s」（%s）连续 %d 次探活失败，解析可能变慢",
					u.Name, u.Addrs, u.Fails),
				Component: "upstream", Count: u.Fails, LastSeen: c.now(),
			}
			if dnsProbeFailed {
				// 抑制规则（M6-049）：DNS 服务不可用时，上游探活失败是次级现象。
				ev.Notify = false
				ev.SuppressedBy = "抑制：DNS 服务不可用"
				ev.Decision = `{"suppressedBy":"抑制：DNS 服务不可用","final":"silent"}`
			}
			report(ev)
		}
	}

	if len(managed) > 0 {
		if _, err := c.svc.ResolveStaleSystem(ctx, managed, dedup(faulty)); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return reported, firstErr
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func dedup(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
