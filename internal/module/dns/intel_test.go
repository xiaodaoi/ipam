package dns

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaodaoi/ipam/internal/pkg/rpz"
)

// intelStack 组装「封禁业务 + RPZ 编译」测试栈（M6-038）。
type intelStack struct {
	svc   *BlocklistService
	repo  *MemBlocklistRepo
	intel *IntelService
	ctl   *fakeCtl
	store *MemIntelRepo
	dir   string
}

func newIntelStack(t *testing.T) *intelStack {
	t.Helper()
	repo := NewMemBlocklistRepo()
	fc := &fakeCtl{}
	store := NewMemIntelRepo()
	dir := t.TempDir()
	fc.zoneDir = dir // 模拟真实 unbound：热载后按 zonefile 内容回报 serial
	intel := NewIntelService(dir, fc, repo, store)
	svc := NewBlocklistService(repo, nil, fc, dir)
	svc.SetIntelService(intel)
	return &intelStack{svc: svc, repo: repo, intel: intel, ctl: fc, store: store, dir: dir}
}

func (s *intelStack) zone(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(s.dir, name+".zone"))
	if err != nil {
		t.Fatalf("读取 zone %s 失败: %v", name, err)
	}
	return string(b)
}

func (s *intelStack) seed(t *testing.T, pattern, action string, schedule *Schedule) *PolicyGroup {
	t.Helper()
	ctx := context.Background()
	bl, err := s.repo.Create(ctx, Blocklist{Name: "测试名单", Kind: "custom"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.repo.UpsertEntries(ctx, []Entry{
		{ListID: bl.ID, TriggerType: "qname", Pattern: pattern, Action: action},
	}); err != nil {
		t.Fatal(err)
	}
	g, err := s.repo.CreatePolicyGroup(ctx, PolicyGroup{
		Name: "g", ViewName: "v", Cidrs: []string{"10.0.0.0/8"}, ListIDs: []string{bl.ID}, Schedule: schedule,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &g
}

func TestIntel_白名单优先于封禁(t *testing.T) {
	st := newIntelStack(t)
	ctx := context.Background()
	if _, err := st.store.CreateWhitelist(ctx, IntelWhitelist{
		Kind: "suffix", Value: "example.com", Reason: "误报", Source: "manual", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	st.seed(t, "www.example.com", "nxdomain", nil)

	if _, err := st.svc.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	wl := st.zone(t, ZoneWhitelist)
	if !strings.Contains(wl, "example.com CNAME rpz-passthru.") ||
		!strings.Contains(wl, "*.example.com CNAME rpz-passthru.") {
		t.Fatalf("白名单区内容不符:\n%s", wl)
	}
	custom := st.zone(t, ZoneCustom)
	if strings.Contains(custom, "www.example.com") {
		t.Fatalf("被白名单覆盖的条目不应进入封禁区:\n%s", custom)
	}
}

func TestIntel_非qname条目跳过(t *testing.T) {
	st := newIntelStack(t)
	ctx := context.Background()
	bl, _ := st.repo.Create(ctx, Blocklist{Name: "l", Kind: "custom"})
	_, _ = st.repo.UpsertEntries(ctx, []Entry{
		{ListID: bl.ID, TriggerType: "response_ip", Pattern: "1.2.3.0/24", Action: "nxdomain"},
		{ListID: bl.ID, TriggerType: "qname", Pattern: "bad.example", Action: "nxdomain"},
	})
	_, _ = st.repo.CreatePolicyGroup(ctx, PolicyGroup{
		Name: "g", ViewName: "v", Cidrs: []string{"10.0.0.0/8"}, ListIDs: []string{bl.ID},
	})
	if _, err := st.svc.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	custom := st.zone(t, ZoneCustom)
	if strings.Contains(custom, "1.2.3.0") {
		t.Fatalf("response_ip 条目不应写入 QNAME 区:\n%s", custom)
	}
	if !strings.Contains(custom, "bad.example CNAME .") {
		t.Fatalf("qname 条目缺失:\n%s", custom)
	}
}

// 模拟 unbound 重启：运行态丢失（serials 为空），下一轮必须重新下发而不是「以为已下发」。
func TestIntel_运行态丢失自动补齐(t *testing.T) {
	st := newIntelStack(t)
	ctx := context.Background()
	st.seed(t, "bad.example", "nxdomain", nil)
	if _, err := st.svc.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	before := st.ctl.zoneReloads
	if before == 0 {
		t.Fatal("首轮应触发热载")
	}
	// 再次收敛：内容与 serial 未变 → 不重复下发（幂等）。
	if _, err := st.svc.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	// 运行态丢失（unbound 重启）：serials 为空 → 应重新下发 4 个区。
	st.ctl.serials = nil
	if _, err := st.svc.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if st.ctl.zoneReloads <= before {
		t.Fatalf("运行态丢失后未重新下发：before=%d after=%d", before, st.ctl.zoneReloads)
	}
}

// 内容未变时不得重复热载：serial=now 的朴素实现会让每分钟调度都整区重载。
func TestIntel_内容未变不重复热载(t *testing.T) {
	st := newIntelStack(t)
	ctx := context.Background()
	st.seed(t, "stable.example", "nxdomain", nil)
	if _, err := st.svc.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	first := st.ctl.zoneReloads
	before := st.zone(t, ZoneCustom)

	if _, err := st.svc.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if st.ctl.zoneReloads != first {
		t.Fatalf("内容未变不应重复热载：first=%d now=%d", first, st.ctl.zoneReloads)
	}
	if st.zone(t, ZoneCustom) != before {
		t.Fatalf("内容未变不应改写 zone 文件（serial 会抖动）")
	}
}

// 热载失败必须回滚到上一版内容，且不中断其他区。
func TestIntel_热载失败回滚(t *testing.T) {
	st := newIntelStack(t)
	ctx := context.Background()
	st.seed(t, "bad.example", "nxdomain", nil)
	if _, err := st.svc.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	prev := st.zone(t, ZoneCustom)

	// 新增条目，但让自定义区热载失败。
	bls, _ := st.repo.List(ctx)
	_, _ = st.repo.UpsertEntries(ctx, []Entry{
		{ListID: bls[0].ID, TriggerType: "qname", Pattern: "new.example", Action: "nxdomain"},
	})
	st.ctl.failReloadZone = ZoneCustom
	if _, err := st.svc.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(st.zone(t, ZoneCustom), "new.example") {
		t.Fatalf("热载失败后应回滚到上一版内容")
	}
	if st.zone(t, ZoneCustom) != prev {
		t.Fatalf("回滚内容与上一版不一致")
	}
	zones, _ := st.store.ListZones(ctx)
	for _, z := range zones {
		if z.Zone == ZoneCustom && z.Status != "failed" {
			t.Fatalf("失败区状态应为 failed，实际 %s", z.Status)
		}
	}
}

// 运行态 serial 与期望不一致（例如 unbound 读到了坏文件）→ 必须回滚。
func TestIntel_serial不一致回滚(t *testing.T) {
	st := newIntelStack(t)
	ctx := context.Background()
	st.seed(t, "bad.example", "nxdomain", nil)
	if _, err := st.svc.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	prev := st.zone(t, ZoneCustom)
	// 关键：热载命令成功，但运行态 serial 与期望不一致（如 unbound 读到了坏文件）。
	st.ctl.serialMismatchZone = ZoneCustom
	bls, _ := st.repo.List(ctx)
	_, _ = st.repo.UpsertEntries(ctx, []Entry{
		{ListID: bls[0].ID, TriggerType: "qname", Pattern: "other.example", Action: "nxdomain"},
	})
	if _, err := st.svc.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(st.zone(t, ZoneCustom), "other.example") {
		t.Fatalf("serial 校验失败应回滚")
	}
	if st.zone(t, ZoneCustom) != prev {
		t.Fatalf("回滚内容与上一版不一致")
	}
}

func TestIntel_白名单增删立即生效(t *testing.T) {
	st := newIntelStack(t)
	ctx := context.Background()
	st.seed(t, "bad.example", "nxdomain", nil)
	if _, err := st.svc.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	w, _, err := st.intel.AddWhitelist(ctx, IntelWhitelist{
		Kind: "qname", Value: "bad.example", Reason: "误报", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(st.zone(t, ZoneWhitelist), "bad.example CNAME rpz-passthru.") {
		t.Fatalf("新增白名单未落区:\n%s", st.zone(t, ZoneWhitelist))
	}
	if strings.Contains(st.zone(t, ZoneCustom), "bad.example") {
		t.Fatalf("新增白名单后封禁区仍含该名")
	}
	if len(st.ctl.flushes) == 0 || st.ctl.flushes[0] != "bad.example" {
		t.Fatalf("应清除该名缓存，实际 %v", st.ctl.flushes)
	}

	if _, _, err := st.intel.DeleteWhitelist(ctx, w.ID); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(st.zone(t, ZoneWhitelist), "bad.example") {
		t.Fatalf("删除白名单未生效:\n%s", st.zone(t, ZoneWhitelist))
	}
	if !strings.Contains(st.zone(t, ZoneCustom), "bad.example CNAME .") {
		t.Fatalf("删除白名单后应恢复拦截:\n%s", st.zone(t, ZoneCustom))
	}
}

// 新增封禁条目应立即清掉该名缓存（否则已解析过的域名最长 5 分钟仍可访问）。
func TestIntel_新增封禁立即清缓存(t *testing.T) {
	st := newIntelStack(t)
	ctx := context.Background()
	st.seed(t, "cache.example", "nxdomain", nil)
	if _, err := st.svc.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	bls, _ := st.repo.List(ctx)
	_, _ = st.repo.UpsertEntries(ctx, []Entry{
		{ListID: bls[0].ID, TriggerType: "qname", Pattern: "newblock.example", Action: "nxdomain"},
	})
	if _, err := st.svc.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range st.ctl.flushes {
		if n == "newblock.example" {
			found = true
		}
	}
	if !found {
		t.Fatalf("新增条目应清缓存，实际 flushes=%v", st.ctl.flushes)
	}
}

// 变更量超过上限时不做逐条 flush（避免万级条目把一次编译拖成分钟级）。
func TestIntel_大规模变更跳过flush(t *testing.T) {
	st := newIntelStack(t)
	ctx := context.Background()
	if err := st.intel.EnsureFiles(ctx); err != nil {
		t.Fatal(err)
	}
	// 构造 300 条新增，超过 flushCap(200)。
	entries := make([]rpz.Record, 0, 300)
	for i := 0; i < 300; i++ {
		entries = append(entries, rpz.Record{Name: fmt.Sprintf("b%d.example", i), Action: rpz.ActionNXDomain})
	}
	before, _ := rpz.Render(ZoneCustom, 1, nil)
	after, _ := rpz.Render(ZoneCustom, 2, entries)
	st.ctl.flushes = nil
	st.intel.flushChanges(before, after)
	if len(st.ctl.flushes) != 0 {
		t.Fatalf("超上限不应逐条 flush，实际 %d 条", len(st.ctl.flushes))
	}
}

func TestIntel_白名单校验(t *testing.T) {
	st := newIntelStack(t)
	ctx := context.Background()
	if _, _, err := st.intel.AddWhitelist(ctx, IntelWhitelist{Kind: "qname", Value: "bad.example"}); !errors.Is(err, ErrWhitelistInvalid) {
		t.Fatalf("缺原因应拒绝，got %v", err)
	}
	if _, _, err := st.intel.AddWhitelist(ctx, IntelWhitelist{Kind: "qname", Value: "bad name", Reason: "x"}); !errors.Is(err, ErrWhitelistInvalid) {
		t.Fatalf("非法域名应拒绝，got %v", err)
	}
	if _, _, err := st.intel.AddWhitelist(ctx, IntelWhitelist{Kind: "qname", Value: "ok.example", Reason: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.intel.AddWhitelist(ctx, IntelWhitelist{Kind: "qname", Value: "OK.example", Reason: "x"}); !errors.Is(err, ErrWhitelistDup) {
		t.Fatalf("重复应冲突，got %v", err)
	}
}

func TestIntel_EnsureFiles写空区(t *testing.T) {
	st := newIntelStack(t)
	if err := st.intel.EnsureFiles(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, zone := range RPZZoneOrder {
		text := st.zone(t, zone)
		if !strings.Contains(text, "SOA localhost.") || strings.Contains(text, "CNAME") {
			t.Fatalf("空区内容不符 %s:\n%s", zone, text)
		}
	}
}

func TestIntel_序列号单调递增(t *testing.T) {
	now := time.Unix(1700000000, 0)
	if got := nextSerial(0, now); got != 1700000000 {
		t.Fatalf("首轮 serial=%d", got)
	}
	if got := nextSerial(1700000000, now); got != 1700000001 {
		t.Fatalf("同秒应 +1，got %d", got)
	}
	if got := nextSerial(1700000005, now); got != 1700000006 {
		t.Fatalf("时间回退应保持单调（prev+1），got %d", got)
	}
}
