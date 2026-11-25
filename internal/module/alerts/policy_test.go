package alerts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaodaoi/ipam/internal/module/logquery"
)

// Test策略解析默认降噪与系统健康护栏 覆盖 §M6-049 默认姿态：
// broad 源归档、hot 源通知、多源取最高级别且任一 notify 则通知。
func Test策略解析默认降噪与系统健康护栏(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()

	// 纯 StevenBlack：info / digest / 按来源聚合，不进入通知层。
	eff := svc.ResolvePolicy(ctx, PolicyInput{
		Source: SourceDNS, Kind: KindRiskURL, IntelSources: []string{"stevenblack-hosts"},
	})
	if eff.Notify || eff.Severity != SeverityInfo || eff.NotifyMode != NotifyModeDigest {
		t.Fatalf("StevenBlack 应归档：%+v", eff)
	}
	if eff.GroupBy != GroupBySource || eff.AlertClass != ClassPolicyBlock {
		t.Fatalf("StevenBlack 应按来源聚合且归类策略封禁：%+v", eff)
	}

	// 纯 ThreatFox：critical / notify。
	eff = svc.ResolvePolicy(ctx, PolicyInput{
		Source: SourceDNS, Kind: KindRiskURL, IntelSources: []string{"abusech-threatfox"},
	})
	if !eff.Notify || eff.Severity != SeverityCritical || eff.NotifyMode != NotifyModeNotify {
		t.Fatalf("ThreatFox 应通知：%+v", eff)
	}

	// 多源命中：最高级别 + 任一 notify。
	eff = svc.ResolvePolicy(ctx, PolicyInput{
		Source: SourceDNS, Kind: KindRiskURL,
		IntelSources: []string{"stevenblack-hosts", "abusech-threatfox"},
	})
	if !eff.Notify || eff.Severity != SeverityCritical {
		t.Fatalf("多源应取最高级别并通知：%+v", eff)
	}

	// 系统健康 critical 即使被改成 off 也必须通知。
	eff = svc.ResolvePolicy(ctx, PolicyInput{Source: SourceSystem, Kind: KindDBDown, Component: "database"})
	if !eff.Notify || eff.Severity != SeverityCritical {
		t.Fatalf("系统健康 critical 默认应通知：%+v", eff)
	}
}

// TestStevenBlack按来源聚合且不产生未读 验证 broad 源不再每域名一条，也不刷铃铛。
func TestStevenBlack按来源聚合且不产生未读(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()
	if err := svc.EnsurePolicies(ctx); err != nil {
		t.Fatal(err)
	}
	bucket := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	src := &fakeDNS{groups: []logquery.BlockedGroup{
		{Bucket: bucket, ClientIP: "10.0.0.5", Domain: "a.bad.example", Count: 3,
			FirstSeen: bucket.Add(5 * time.Minute), LastSeen: bucket.Add(20 * time.Minute)},
		{Bucket: bucket, ClientIP: "10.0.0.5", Domain: "b.bad.example", Count: 2,
			FirstSeen: bucket.Add(6 * time.Minute), LastSeen: bucket.Add(21 * time.Minute)},
	}}
	col := newTestCollector(svc, SystemChecks{})
	col.SetDNSSource(src)
	col.SetAttributor(&fakeAttr{keys: "stevenblack-hosts", names: "StevenBlack 综合屏蔽"})

	n, err := col.CollectDNS(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("两个域名应聚合为 1 条报警，got %d", n)
	}
	page, err := svc.List(ctx, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 {
		t.Fatalf("应只有 1 条报警，got %d", page.Total)
	}
	a := page.Items[0]
	if a.DedupKey != "dns|source|stevenblack-hosts|10.0.0.5" {
		t.Fatalf("聚合键不符: %s", a.DedupKey)
	}
	if a.Notify || a.Severity != SeverityInfo || a.AlertClass != ClassPolicyBlock {
		t.Fatalf("broad 源应归档：%+v", a)
	}
	if a.Domain != "" || a.Count != 5 {
		t.Fatalf("聚合后不应保留单域名，计数应累加：%+v", a)
	}

	if unread, _ := svc.Unread(ctx, 8); unread.Count != 0 {
		t.Fatalf("归档报警不应计入铃铛，got %d", unread.Count)
	}
	if page, _ := svc.List(ctx, Filter{Notify: boolPtr(false)}); page.Total != 1 {
		t.Fatalf("按归档过滤应命中，got %d", page.Total)
	}
	if page, _ := svc.List(ctx, Filter{Notify: boolPtr(true)}); page.Total != 0 {
		t.Fatalf("按需关注过滤不应命中，got %d", page.Total)
	}
	st, _ := svc.Stats(ctx, Filter{})
	if st.Notified != 0 || st.Archived != 1 {
		t.Fatalf("统计通知/归档不符：%+v", st)
	}
}

func boolPtr(v bool) *bool { return &v }

// Test确认后冷却与重复提醒 验证稳定指纹下 ack 不会下一小时复活，只有超时或激增才重开。
func Test确认后冷却与重复提醒(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()
	base := fixedNow()
	mk := func(last time.Time, count int) Event {
		d := "dns|domain|abusech-threatfox|10.0.0.9|evil.example"
		return Event{
			DedupKey: d, Fingerprint: d, Source: SourceDNS, Kind: KindRiskURL,
			Severity: SeverityCritical, Title: "风险域名访问：evil.example", Domain: "evil.example",
			ClientIP: "10.0.0.9", Count: count, Notify: true, AlertClass: ClassSecurityThreat,
			PolicyID: "p1", Decision: `{"notify":true,"final":"notify"}`,
			RepeatAfterSec: 3600, ResolveAfterSec: 0, FirstSeen: last, LastSeen: last,
		}
	}
	a, err := svc.Report(ctx, mk(base, 1))
	if err != nil {
		t.Fatal(err)
	}
	if !a.Notify || a.Status != StatusOpen {
		t.Fatalf("首次上报应通知且未确认：%+v", a)
	}
	if _, err := svc.Ack(ctx, []string{a.ID}, "operator"); err != nil {
		t.Fatal(err)
	}

	// 冷却期内重复命中：计数更新但保持已确认，不再通知。
	mid, _ := svc.Report(ctx, mk(base.Add(10*time.Minute), 2))
	if mid.Status != StatusAcked || mid.JustNotified {
		t.Fatalf("冷却期内不应重新通知：%+v", mid)
	}
	if mid.Count != 2 {
		t.Fatalf("冷却期内计数仍应合并到 2，got %d", mid.Count)
	}

	// 超过 repeat_after：重新打开并再次通知。
	again, _ := svc.Report(ctx, mk(base.Add(2*time.Hour), 3))
	if again.Status != StatusOpen || !again.JustNotified {
		t.Fatalf("超过冷却应重新通知：%+v", again)
	}
	if again.AckedAt != nil || again.AckedBy != "" {
		t.Fatalf("重开应清除确认信息：%+v", again)
	}
	if again.NotifyCount != 2 {
		t.Fatalf("通知次数应为 2，got %d", again.NotifyCount)
	}
}

// Test未读仅统计通知层 验证铃铛与 unread 只统计 notify=true 的 open 报警。
func Test未读仅统计通知层(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()
	now := fixedNow()
	events := []Event{
		{DedupKey: "dns|domain|abusech-threatfox|10.0.0.1|evil.example", Source: SourceDNS, Kind: KindRiskURL,
			Severity: SeverityCritical, Title: "威胁", Domain: "evil.example", ClientIP: "10.0.0.1",
			Notify: true, AlertClass: ClassSecurityThreat, PolicyID: "p1", Decision: `{"notify":true}`,
			LastSeen: now},
		{DedupKey: "dns|source|stevenblack-hosts|10.0.0.2", Source: SourceDNS, Kind: KindRiskURL,
			Severity: SeverityInfo, Title: "聚合归档", ClientIP: "10.0.0.2",
			Notify: false, AlertClass: ClassPolicyBlock, PolicyID: "p2", Decision: `{"notify":false,"final":"digest"}`,
			LastSeen: now},
	}
	for _, ev := range events {
		if _, err := svc.Report(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	unread, err := svc.Unread(ctx, 8)
	if err != nil {
		t.Fatal(err)
	}
	if unread.Count != 1 || len(unread.Items) != 1 || !unread.Items[0].Notify {
		t.Fatalf("未读应只含通知层报警：%+v", unread)
	}
	st, _ := svc.Stats(ctx, Filter{})
	if st.Notified != 1 || st.Archived != 1 {
		t.Fatalf("统计不符：%+v", st)
	}
}

// Test静音窗口阻止通知但保留档案 验证静音命中后落库可查，但不进入通知层。
func Test静音窗口阻止通知但保留档案(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()
	if err := svc.EnsurePolicies(ctx); err != nil {
		t.Fatal(err)
	}
	_, err := svc.CreateSilence(ctx, Silence{
		Matchers: SilenceMatchers{IntelSource: "abusech-threatfox"},
		StartsAt: fixedNow().Add(-time.Minute), EndsAt: fixedNow().Add(time.Hour),
		Reason: "已知红队演练",
	}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	eff := svc.ResolvePolicy(ctx, PolicyInput{
		Source: SourceDNS, Kind: KindRiskURL, IntelSources: []string{"abusech-threatfox"},
	})
	if eff.Notify || !strings.Contains(eff.SuppressedBy, "静音") || eff.Final != NotifyModeSilent {
		t.Fatalf("静音应压制通知：%+v", eff)
	}
	ev := Event{
		DedupKey: "dns|domain|abusech-threatfox|10.0.0.7|evil.example", Source: SourceDNS, Kind: KindRiskURL,
		Severity: eff.Severity, Title: "威胁", Domain: "evil.example", ClientIP: "10.0.0.7",
		Notify: eff.Notify, AlertClass: eff.AlertClass, PolicyID: eff.ID,
		SuppressedBy: eff.SuppressedBy, SilenceID: eff.SilenceID, Decision: DecisionOf(eff),
		LastSeen: fixedNow(),
	}
	a, err := svc.Report(ctx, ev)
	if err != nil {
		t.Fatal(err)
	}
	if a.Notify || !strings.Contains(a.SuppressedBy, "静音") {
		t.Fatalf("静音报警应归档：%+v", a)
	}
	if unread, _ := svc.Unread(ctx, 8); unread.Count != 0 {
		t.Fatalf("静音报警不应计入铃铛，got %d", unread.Count)
	}
	if page, _ := svc.List(ctx, Filter{}); page.Total != 1 {
		t.Fatalf("静音期间仍应落库，got %d", page.Total)
	}
}

// Test每小时通知上限超限合并归档 验证单源预算耗尽后后续报警降级为归档。
func Test每小时通知上限超限合并归档(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()
	if err := svc.EnsurePolicies(ctx); err != nil {
		t.Fatal(err)
	}
	list, _ := svc.Policies(ctx)
	var target Policy
	for _, p := range list {
		if p.ScopeType == ScopeIntelSource && p.ScopeKey == "abusech-threatfox" {
			target = p
		}
	}
	if target.ID == "" {
		t.Fatal("未找到 ThreatFox 默认策略")
	}
	limit := 1
	if _, err := svc.UpdatePolicy(ctx, target.ID, PolicyPatch{MaxPerHour: &limit}, "operator"); err != nil {
		t.Fatal(err)
	}

	bucket := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	src := &fakeDNS{groups: []logquery.BlockedGroup{
		{Bucket: bucket, ClientIP: "10.0.0.8", Domain: "one.bad.example", Count: 5,
			FirstSeen: bucket, LastSeen: bucket.Add(time.Minute)},
		{Bucket: bucket, ClientIP: "10.0.0.8", Domain: "two.bad.example", Count: 4,
			FirstSeen: bucket, LastSeen: bucket.Add(time.Minute)},
	}}
	col := newTestCollector(svc, SystemChecks{})
	col.SetDNSSource(src)
	col.SetAttributor(&fakeAttr{keys: "abusech-threatfox", names: "abuse.ch ThreatFox"})
	if n, err := col.CollectDNS(ctx); err != nil || n != 2 {
		t.Fatalf("采集 n=%d err=%v", n, err)
	}

	page, _ := svc.List(ctx, Filter{})
	notified, capped := 0, 0
	for _, a := range page.Items {
		if a.Notify {
			notified++
		} else if strings.Contains(a.SuppressedBy, "限流") {
			capped++
			if !strings.Contains(a.Decision, `"capped":true`) {
				t.Fatalf("超限报警决策链应标记 capped：%s", a.Decision)
			}
		}
	}
	if notified != 1 || capped != 1 {
		t.Fatalf("应 1 条通知、1 条合并归档，got notified=%d capped=%d", notified, capped)
	}
}

// Test策略模拟估算通知与漏报 验证模拟器只读回放，不改变运行态。
func Test策略模拟估算通知与漏报(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()
	now := fixedNow()
	for i, ev := range []Event{
		{DedupKey: "dns|domain|abusech-threatfox|10.0.0.1|a.example", Source: SourceDNS, Kind: KindRiskURL,
			Severity: SeverityCritical, Title: "A", Domain: "a.example", ClientIP: "10.0.0.1",
			Notify: true, AlertClass: ClassSecurityThreat, PolicyID: "p", Decision: `{"notify":true}`, LastSeen: now},
		{DedupKey: "dns|domain|abusech-threatfox|10.0.0.1|b.example", Source: SourceDNS, Kind: KindRiskURL,
			Severity: SeverityCritical, Title: "B", Domain: "b.example", ClientIP: "10.0.0.1",
			Notify: true, AlertClass: ClassSecurityThreat, PolicyID: "p", Decision: `{"notify":true}`, LastSeen: now},
		{DedupKey: "dns|domain|stevenblack-hosts|10.0.0.2|c.example", Source: SourceDNS, Kind: KindRiskURL,
			Severity: SeverityWarning, Title: "C", Domain: "c.example", ClientIP: "10.0.0.2",
			Notify: true, AlertClass: ClassPolicyBlock, PolicyID: "p", Decision: `{"notify":true}`, LastSeen: now},
	} {
		if _, err := svc.Report(ctx, ev); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	res, err := svc.SimulatePolicy(ctx, Policy{
		Name: "候选-全局归档", ScopeType: ScopeGlobal, Severity: SeverityInfo,
		NotifyMode: NotifyModeDigest, AlertClass: ClassPolicyBlock, GroupBy: GroupBySource, Enabled: true,
	}, now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if res.Recorded != 3 || res.Notified != 0 || res.Archived != 3 || res.BySeverity[SeverityInfo] != 3 {
		t.Fatalf("模拟结果不符：%+v", res)
	}
	// 运行态不变。
	if page, _ := svc.List(ctx, Filter{Notify: boolPtr(true)}); page.Total != 3 {
		t.Fatalf("模拟不应改写运行态，got %d", page.Total)
	}
}

// Test通知通道密钥落文件且可清除 验证密钥 0600 落盘、不回传、可清除。
func Test通知通道密钥落文件且可清除(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService()
	dir := t.TempDir()
	svc.SetSecretDir(dir)
	ch, err := svc.CreateChannel(ctx, Channel{
		Name: "安全 Webhook", Type: ChannelWebhook, Enabled: true,
		Config: ChannelConfig{URL: "https://example.com/hook"},
	}, "topsecret", "operator")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ch.ID+".secret")
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "topsecret" {
		t.Fatalf("密钥应写入文件：err=%v raw=%q", err, raw)
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("密钥文件权限应为 0600：%v %v", err, st)
	}
	if ch.SecretFile != path {
		t.Fatalf("通道应记录密钥文件路径，got %q", ch.SecretFile)
	}
	if _, err := svc.UpdateChannel(ctx, ch.ID, ChannelPatch{}, "", true, "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("清除密钥应删除文件：%v", err)
	}
}
