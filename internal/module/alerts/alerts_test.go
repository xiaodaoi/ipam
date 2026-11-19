package alerts

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xiaodaoi/ipam/internal/module/logquery"
)

func fixedNow() time.Time {
	return time.Date(2026, 11, 19, 10, 30, 0, 0, time.UTC)
}

func newTestService() (*Service, *MemRepo) {
	repo := NewMemRepo()
	svc := NewService(repo)
	svc.SetNow(fixedNow)
	return svc, repo
}

func newTestCollector(svc *Service, checks SystemChecks) *Collector {
	c := NewCollector(svc, checks)
	c.SetLogger(func(string, ...any) {})
	c.SetNow(fixedNow)
	return c
}

type fakeDNS struct {
	groups []logquery.BlockedGroup
	calls  int
}

func (f *fakeDNS) BlockedEvents(context.Context, time.Time) ([]logquery.BlockedGroup, error) {
	f.calls++
	return f.groups, nil
}

func TestDNS风险访问按小时聚合且重复扫描不重复计数(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()
	src := &fakeDNS{groups: []logquery.BlockedGroup{{
		Bucket:    time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC),
		ClientIP:  "10.61.172.10",
		Domain:    "J8.ASDLOT.ORG",
		Category:  "intel.hot",
		Count:     3,
		FirstSeen: time.Date(2026, 11, 19, 10, 5, 0, 0, time.UTC),
		LastSeen:  time.Date(2026, 11, 19, 10, 20, 0, 0, time.UTC),
	}}}
	col := newTestCollector(svc, SystemChecks{})
	col.SetDNSSource(src)

	if n, err := col.CollectDNS(ctx); err != nil || n != 1 {
		t.Fatalf("首次采集 n=%d err=%v", n, err)
	}
	// 同一小时桶累计值上升：仍应只有 1 条，计数取较大值。
	src.groups[0].Count = 5
	if n, err := col.CollectDNS(ctx); err != nil || n != 1 {
		t.Fatalf("二次采集 n=%d err=%v", n, err)
	}

	page, err := svc.List(ctx, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("应只有 1 条报警，got total=%d items=%d", page.Total, len(page.Items))
	}
	a := page.Items[0]
	if a.Count != 5 {
		t.Fatalf("计数应为 5（GREATEST 合并），got %d", a.Count)
	}
	if a.ClientIP != "10.61.172.10" || a.Domain != "j8.asdlot.org" {
		t.Fatalf("客户端/域名不符: %+v", a)
	}
	if !strings.Contains(a.DedupKey, "2026111910") {
		t.Fatalf("小时桶键不符: %s", a.DedupKey)
	}

	unread, err := svc.Unread(ctx, 8)
	if err != nil || unread.Count != 1 {
		t.Fatalf("未确认计数应为 1，got %d err=%v", unread.Count, err)
	}
	// 查看即确认：确认后徽标归零，重复确认仍幂等。
	if n, err := svc.Ack(ctx, []string{a.ID}, "operator"); err != nil || n != 1 {
		t.Fatalf("确认 n=%d err=%v", n, err)
	}
	if n, _ := svc.Ack(ctx, []string{a.ID}, "operator"); n != 0 {
		t.Fatalf("重复确认应为 0，got %d", n)
	}
	if unread, _ := svc.Unread(ctx, 8); unread.Count != 0 {
		t.Fatalf("确认后未确认计数应为 0，got %d", unread.Count)
	}

	st, err := svc.Stats(ctx, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Total != 1 || st.Acked != 1 || st.Open != 0 {
		t.Fatalf("统计不符: %+v", st)
	}
	if len(st.TopDomains) != 1 || st.TopDomains[0].Key != "j8.asdlot.org" || st.TopDomains[0].Count != 5 {
		t.Fatalf("Top 域名不符: %+v", st.TopDomains)
	}
	if len(st.TopClients) != 1 || st.TopClients[0].Key != "10.61.172.10" {
		t.Fatalf("Top 客户端不符: %+v", st.TopClients)
	}
}

func Test系统故障连续失败才报警且恢复自动解决(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()
	down := true
	col := newTestCollector(svc, SystemChecks{
		Postgres: func(context.Context) error {
			if down {
				return errors.New("connection refused")
			}
			return nil
		},
	})

	// 第 1 次失败不报警（避免抖动误报）。
	if _, err := col.CollectSystem(ctx); err != nil {
		t.Fatal(err)
	}
	if page, _ := svc.List(ctx, Filter{}); page.Total != 0 {
		t.Fatalf("首次失败不应产生报警，got %d", page.Total)
	}
	// 第 2 次失败达到阈值。
	if n, err := col.CollectSystem(ctx); err != nil || n != 1 {
		t.Fatalf("第 2 次采集 n=%d err=%v", n, err)
	}
	page, _ := svc.List(ctx, Filter{})
	if page.Total != 1 {
		t.Fatalf("应产生 1 条报警，got %d", page.Total)
	}
	a := page.Items[0]
	if a.Source != SourceSystem || a.Kind != KindDBDown || a.Severity != SeverityCritical || a.Status != StatusOpen {
		t.Fatalf("报警字段不符: %+v", a)
	}
	if _, err := svc.Ack(ctx, []string{a.ID}, "admin"); err != nil {
		t.Fatal(err)
	}

	// 恢复：自动 resolved，未确认数归零。
	down = false
	if _, err := col.CollectSystem(ctx); err != nil {
		t.Fatal(err)
	}
	page, _ = svc.List(ctx, Filter{})
	if page.Items[0].Status != StatusResolved || page.Items[0].ResolvedAt == nil {
		t.Fatalf("恢复后应为 resolved: %+v", page.Items[0])
	}
	if unread, _ := svc.Unread(ctx, 8); unread.Count != 0 {
		t.Fatalf("恢复后未确认应为 0，got %d", unread.Count)
	}

	// 再次发生：重新打开为未确认，并清除上次确认信息。
	down = true
	if _, err := col.CollectSystem(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := col.CollectSystem(ctx); err != nil {
		t.Fatal(err)
	}
	page, _ = svc.List(ctx, Filter{})
	if page.Total != 1 {
		t.Fatalf("同一组件不应重复建行，got %d", page.Total)
	}
	got := page.Items[0]
	if got.Status != StatusOpen || got.AckedAt != nil || got.AckedBy != "" || got.ResolvedAt != nil {
		t.Fatalf("再次发生应重新打开且清除确认信息: %+v", got)
	}
}

func Test磁盘水位报警与恢复(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()
	pct := 92.0
	col := newTestCollector(svc, SystemChecks{
		Disks: func() []DiskUsage {
			return []DiskUsage{{Mount: "/", UsedPct: pct, TotalBytes: 100, UsedBytes: uint64(pct)}}
		},
	})
	if n, err := col.CollectSystem(ctx); err != nil || n != 1 {
		t.Fatalf("磁盘报警 n=%d err=%v", n, err)
	}
	page, _ := svc.List(ctx, Filter{Kind: KindDiskPressure})
	if page.Total != 1 || page.Items[0].Component != "host" {
		t.Fatalf("磁盘报警不符: %+v", page.Items)
	}
	pct = 50
	if _, err := col.CollectSystem(ctx); err != nil {
		t.Fatal(err)
	}
	page, _ = svc.List(ctx, Filter{Kind: KindDiskPressure})
	if page.Items[0].Status != StatusResolved {
		t.Fatalf("水位回落后应 resolved: %+v", page.Items[0])
	}
}

func Test未配置的探针不会被误判为恢复(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()
	// 先手工造一条 db_down 报警。
	if _, err := svc.Report(ctx, Event{
		DedupKey: "system|db_down|database", Source: SourceSystem, Kind: KindDBDown,
		Severity: SeverityCritical, Title: "数据库连接异常", Component: "database",
	}); err != nil {
		t.Fatal(err)
	}
	// 本轮只启用磁盘探针：db_down 不在 managedKinds，不能被自动 resolved。
	col := newTestCollector(svc, SystemChecks{
		Disks: func() []DiskUsage { return nil },
	})
	if _, err := col.CollectSystem(ctx); err != nil {
		t.Fatal(err)
	}
	page, _ := svc.List(ctx, Filter{Kind: KindDBDown})
	if page.Items[0].Status != StatusOpen {
		t.Fatalf("未启用的探针不应被 resolved: %+v", page.Items[0])
	}
}

func Test列表过滤与模糊搜索(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()
	for _, ev := range []Event{
		{DedupKey: "dns|10.0.0.1|evil.example|2026111910", Source: SourceDNS, Kind: KindRiskURL,
			Severity: SeverityWarning, Title: "风险域名访问：evil.example", Domain: "evil.example",
			ClientIP: "10.0.0.1", Count: 2},
		{DedupKey: "system|disk_pressure|/data", Source: SourceSystem, Kind: KindDiskPressure,
			Severity: SeverityWarning, Title: "磁盘空间不足：/data", Component: "host"},
	} {
		if _, err := svc.Report(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}

	if page, _ := svc.List(ctx, Filter{Q: "evil"}); page.Total != 1 || page.Items[0].Domain != "evil.example" {
		t.Fatalf("按域名模糊搜索不符: %+v", page.Items)
	}
	if page, _ := svc.List(ctx, Filter{Q: "10.0.0.1"}); page.Total != 1 {
		t.Fatalf("按客户端地址模糊搜索不符: %+v", page.Items)
	}
	if page, _ := svc.List(ctx, Filter{Source: SourceSystem}); page.Total != 1 || page.Items[0].Kind != KindDiskPressure {
		t.Fatalf("按来源过滤不符: %+v", page.Items)
	}
	if page, _ := svc.List(ctx, Filter{Severity: SeverityCritical}); page.Total != 0 {
		t.Fatalf("按级别过滤不符: %+v", page.Items)
	}
	if _, err := svc.List(ctx, Filter{From: fixedNow().Add(time.Hour), To: fixedNow()}); !errors.Is(err, ErrBadWindow) {
		t.Fatalf("非法时间窗应报 ErrBadWindow，got %v", err)
	}
}
