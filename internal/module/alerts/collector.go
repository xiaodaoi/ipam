package alerts

import (
	"context"
	"fmt"
	"log"
	"strings"
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
	svc    *Service
	dns    DNSSource
	attr   DomainAttributor
	checks SystemChecks
	logf   func(format string, args ...any)
	now    func() time.Time
	fails  map[string]int
	purged time.Time
}

// ServiceFailThreshold 连续失败次数达到该值才产生系统报警（避免网络抖动误报）。
const ServiceFailThreshold = 2

// NewCollector 构造。
func NewCollector(svc *Service, checks SystemChecks) *Collector {
	return &Collector{
		svc:    svc,
		checks: checks,
		logf:   log.Printf,
		now:    func() time.Time { return time.Now().UTC() },
		fails:  map[string]int{},
	}
}

// SetDNSSource 接入 ClickHouse 风险访问聚合。
func (c *Collector) SetDNSSource(src DNSSource) { c.dns = src }

// SetAttributor 接入情报来源归属（源 key/显示名）。
func (c *Collector) SetAttributor(a DomainAttributor) { c.attr = a }

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

// CollectDNS 回扫最近 2 小时被拦截的 DNS 访问，按小时桶合并。
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
	reported := 0
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
		ev := Event{
			DedupKey:        fmt.Sprintf("dns|%s|%s|%s", g.ClientIP, strings.ToLower(g.Domain), bucket.Format("2006010215")),
			Source:          SourceDNS,
			Kind:            KindRiskURL,
			Severity:        SeverityWarning,
			Title:           fmt.Sprintf("风险域名访问：%s", g.Domain),
			Message:         fmt.Sprintf("客户端 %s 在 %s 这一小时内访问被拦截域名 %d 次", g.ClientIP, bucket.Format("2006-01-02 15:00"), g.Count),
			ClientIP:        g.ClientIP,
			Domain:          domain,
			IntelSource:     intelKey,
			IntelSourceName: intelName,
			Count:           g.Count,
			Detail:          g.Category,
			FirstSeen:       g.FirstSeen.UTC(),
			LastSeen:        g.LastSeen.UTC(),
		}
		if _, err := c.svc.Report(ctx, ev); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		reported++
	}
	return reported, firstErr
}

// CollectSystem 采集系统故障，并把已恢复的报警自动置为 resolved。
func (c *Collector) CollectSystem(ctx context.Context) (int, error) {
	managed := make([]string, 0, 10)
	faulty := make([]string, 0, 16)
	reported := 0
	var firstErr error

	report := func(ev Event) {
		faulty = append(faulty, ev.DedupKey)
		if _, err := c.svc.Report(ctx, ev); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return
		}
		reported++
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
				Title: "日志外发已降级", Message: "磁盘水位或队列积压导致日志外发自动降级，外发可能延迟",
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
			report(Event{
				DedupKey: "system|" + KindUpstreamProbeFailed + "|" + u.ID,
				Source:   SourceSystem, Kind: KindUpstreamProbeFailed, Severity: SeverityWarning,
				Title: fmt.Sprintf("上游 DNS 不可达：%s", u.Name),
				Message: fmt.Sprintf("上游「%s」（%s）连续 %d 次探活失败，解析可能变慢",
					u.Name, u.Addrs, u.Fails),
				Component: "upstream", Count: u.Fails, LastSeen: c.now(),
			})
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
