package dns

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xiaodaoi/ipam/internal/pkg/rpz"
)

// RPZ 区名与顺序（M6-038）。
//
// 顺序即优先级：unbound 按 rpz: 子句出现顺序匹配，先命中即终止。
// 因此白名单必须在第一位；其余顺序固定，运行期禁止增删子句
// （启停通过「写空 zone + auth_zone_reload」实现，避免整机 reload）。
const (
	ZoneWhitelist = "wl.whitelist.rpz"
	ZoneHot       = "intel.hot.rpz"
	ZoneBroad     = "intel.broad.rpz"
	ZoneCustom    = "intel.custom.rpz"
)

// RPZZoneOrder 固定下发顺序（P0：hot/broad 为空区，P1 填外部情报）。
var RPZZoneOrder = []string{ZoneWhitelist, ZoneHot, ZoneBroad, ZoneCustom}

// IntelCompileResult 一次编译的结果（审计/接口返回）。
type IntelCompileResult struct {
	Zones    []IntelZoneState `json:"zones"`
	Entries  int              `json:"entries"`
	Reloaded []string         `json:"reloaded"`
}

// IntelService RPZ 编译与下发（白名单 + 自定义封禁；P1 扩展外部情报源）。
type IntelService struct {
	dir       string
	ctl       UnboundController
	repo      BlocklistRepo
	store     IntelRepo
	blockPage func(context.Context) string
	fetch     SourceFetcher // 情报源拉取（M6-039）
	secretDir string        // 情报源密钥目录（不入库）

	mu  sync.Mutex
	now func() time.Time

	// sourceLocks 按源串行化同步/回滚（M6-058）：此前用单一全局 syncMu，
	// 一个慢源会把所有源同步与人工操作串行拖住。
	lockMu      sync.Mutex
	sourceLocks map[string]*sync.Mutex
	// backoff 失败退避：失败后延迟重试，避免持续超时的源每 5 分钟耗一次 60s 窗口。
	backoffMu sync.Mutex
	backoff   map[string]backoffState

	// 来源归属索引（M6-040）：懒构建，源变更后整体作废。
	attrMu sync.Mutex
	attr   *domainAttribution
}

// NewIntelService 创建编译服务；dir 为 zonefile 目录（容器内 /etc/unbound-rendered/zones/rpz）。
func NewIntelService(dir string, ctl UnboundController, repo BlocklistRepo, store IntelRepo) *IntelService {
	return &IntelService{dir: dir, ctl: ctl, repo: repo, store: store, now: time.Now,
		fetch: HTTPSourceFetch, secretDir: "/run/ipam/intel-secrets",
		sourceLocks: map[string]*sync.Mutex{}, backoff: map[string]backoffState{}}
}

// sourceLock 返回该源的互斥锁（按源粒度，不同源可并行同步）。
func (s *IntelService) sourceLock(id string) *sync.Mutex {
	s.lockMu.Lock()
	defer s.lockMu.Unlock()
	if s.sourceLocks == nil {
		s.sourceLocks = map[string]*sync.Mutex{}
	}
	m, ok := s.sourceLocks[id]
	if !ok {
		m = &sync.Mutex{}
		s.sourceLocks[id] = m
	}
	return m
}

// backoffState 单源失败退避状态。
type backoffState struct {
	failures int
	until    time.Time
}

// backoffUntil 返回该源最早可重试时间（零值=无退避）。
func (s *IntelService) backoffUntil(id string) time.Time {
	s.backoffMu.Lock()
	defer s.backoffMu.Unlock()
	if s.backoff == nil {
		return time.Time{}
	}
	return s.backoff[id].until
}

// recordSyncResult 成功清零退避，失败按 1m→2m→4m…（上限 30m）递增。
func (s *IntelService) recordSyncResult(id string, err error) {
	s.backoffMu.Lock()
	defer s.backoffMu.Unlock()
	if s.backoff == nil {
		s.backoff = map[string]backoffState{}
	}
	st := s.backoff[id]
	if err == nil {
		delete(s.backoff, id)
		return
	}
	st.failures++
	delay := time.Minute << (st.failures - 1)
	if delay > 30*time.Minute || delay <= 0 {
		delay = 30 * time.Minute
	}
	st.until = time.Now().Add(delay)
	s.backoff[id] = st
}

// SetBlockPageResolver 注入提示页 IP 来源（redirect 动作用）。
func (s *IntelService) SetBlockPageResolver(fn func(context.Context) string) { s.blockPage = fn }

// ZonePath 区文件路径（供 conf 渲染引用）。
func (s *IntelService) ZonePath(zone string) string {
	return filepath.Join(s.dir, zone+".zone")
}

// EnsureFiles 首次启动写空区，保证 conf 中的 rpz: 子句总能找到 zonefile
// （缺文件会令 unbound 区加载失败并进入 expired，宁可下发空区）。
func (s *IntelService) EnsureFiles(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, zone := range RPZZoneOrder {
		path := s.ZonePath(zone)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		serial := uint32(s.now().Unix())
		text, n := rpz.Render(zone, serial, nil)
		if err := writeFileAtomic(path, []byte(text)); err != nil {
			return err
		}
		canonical, _ := rpz.Render(zone, 0, nil)
		_ = s.store.UpsertZone(ctx, IntelZoneState{Zone: zone, Entries: n, Serial: serial,
			SHA: sha256Hex(canonical), Path: path, Status: "empty", UpdatedAt: s.now().UTC()})
	}
	return nil
}

// Compile 全量编译 4 个区并热载变化的区；单区失败自动回滚且不中断其他区。
func (s *IntelService) Compile(ctx context.Context) (IntelCompileResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.compileLocked(ctx)
}

func (s *IntelService) compileLocked(ctx context.Context) (IntelCompileResult, error) {
	res := IntelCompileResult{Zones: []IntelZoneState{}, Reloaded: []string{}}
	now := s.now()

	records := map[string][]rpz.Record{}
	wlSet := map[string]bool{}

	wl, err := s.store.ListWhitelist(ctx)
	if err != nil {
		return res, err
	}
	for _, w := range wl {
		if !w.Active(now) {
			continue
		}
		name := normalizeDomain(w.Value)
		if name == "" {
			continue
		}
		rec := rpz.Record{Name: name, Action: rpz.ActionPassthru}
		if w.Kind == "suffix" {
			// Render 会同时输出 name 与 *.name，覆盖该域及子域。
			rec.Name = "*." + name
		}
		records[ZoneWhitelist] = append(records[ZoneWhitelist], rec)
		wlSet[name] = true
	}

	// 情报源（M6-039）：hot/broad 两层由启用源填充；白名单优先剔除。
	if sources, serr := s.store.ListSources(ctx); serr == nil {
		records[ZoneHot] = append(records[ZoneHot], s.feedRecordsForLayer(sources, "hot", wlSet)...)
		records[ZoneBroad] = append(records[ZoneBroad], s.feedRecordsForLayer(sources, "broad", wlSet)...)
	} else {
		log.Printf("[intel] 读取情报源失败（按空层处理）: %v", serr)
	}

	blockIP := ""
	if s.blockPage != nil {
		blockIP = strings.TrimSpace(s.blockPage(ctx))
	}
	groups, err := s.repo.ListPolicyGroups(ctx)
	if err != nil {
		return res, err
	}
	for _, g := range groups {
		if !g.Schedule.Active(now) {
			continue
		}
		entries, err := s.repo.EntriesForLists(ctx, g.ListIDs)
		if err != nil {
			return res, err
		}
		for _, e := range entries {
			// unbound RPZ 只支持 QNAME 与 Response-IP 触发；P0 仅落 QNAME。
			if e.TriggerType != "" && e.TriggerType != "qname" {
				log.Printf("[intel] 跳过非 qname 条目 %s（trigger=%s）", e.Pattern, e.TriggerType)
				continue
			}
			name := normalizeDomain(e.Pattern)
			if name == "" {
				continue
			}
			if whitelisted(name, wlSet) {
				continue
			}
			records[ZoneCustom] = append(records[ZoneCustom], rpz.Record{
				Name: name, Action: mapRPZAction(e.Action), IP: blockIP,
			})
		}
	}

	prev := map[string]IntelZoneState{}
	if list, err := s.store.ListZones(ctx); err == nil {
		for _, z := range list {
			prev[z.Zone] = z
		}
	}
	actual, aerr := s.ctl.AuthZoneSerials(ctx)
	if aerr != nil {
		log.Printf("[intel] 读取运行态失败（按缺失处理）: %v", aerr)
		actual = map[string]uint32{}
	}

	customEntries := 0
	for _, zone := range RPZZoneOrder {
		recs := records[zone]
		if zone == ZoneCustom {
			customEntries = len(recs)
		}
		// 内容指纹不含 SOA serial：内容未变就沿用上一版 serial 并跳过重写，
		// 否则每分钟调度都会因 serial=now 判定「已变化」而整区热载（300k 条目≈3s/次）。
		canonical, count := rpz.Render(zone, 0, recs)
		sum := sha256Hex(canonical)
		serial := prev[zone].Serial
		changed := prev[zone].SHA != sum || serial == 0
		if changed {
			serial = nextSerial(serial, now)
		}
		text, _ := rpz.Render(zone, serial, recs)
		path := s.ZonePath(zone)
		st := IntelZoneState{Zone: zone, Entries: count, Serial: serial, SHA: sum, Path: path,
			Status: "loaded", UpdatedAt: now.UTC()}

		loaded := actual[zone]
		needReload := changed || loaded != serial
		if !needReload {
			st.LoadedSerial = loaded
			st.Status = "loaded"
			_ = s.store.UpsertZone(ctx, st)
			res.Zones = append(res.Zones, st)
			continue
		}

		backup, _ := os.ReadFile(path)
		if err := writeFileAtomic(path, []byte(text)); err != nil {
			return res, err
		}
		if err := s.ctl.AuthZoneReload(ctx, zone); err != nil {
			log.Printf("[intel] 区 %s 热载失败，回滚：%v", zone, err)
			s.rollback(ctx, zone, path, backup)
			st.Status = "failed"
			_ = s.store.UpsertZone(ctx, st)
			res.Zones = append(res.Zones, st)
			continue
		}
		// 内容切换成功：按需清除新增/移除条目的负缓存，使封禁与解封立即生效
		// （缓存最长 300s，不清理会出现「已下发但 dig 仍解析」）。
		s.flushChanges(ctx, string(backup), text)
		if got, gerr := s.ctl.AuthZoneSerials(ctx); gerr == nil && len(got) > 0 {
			zs, ok := got[zone]
			if !ok || zs != serial {
				log.Printf("[intel] 区 %s serial 校验失败（期望 %d，实际 %d），回滚", zone, serial, zs)
				s.rollback(ctx, zone, path, backup)
				st.Status = "failed"
				_ = s.store.UpsertZone(ctx, st)
				res.Zones = append(res.Zones, st)
				continue
			}
			st.LoadedSerial = zs
		} else {
			// 运行态尚不可读（如 unbound 启动中）：保留新数据并标记 stale，
			// 下一轮编译会因 serial 不匹配自动重下发，无需人工介入。
			st.LoadedSerial = serial
			st.Status = "stale"
		}
		res.Reloaded = append(res.Reloaded, zone)
		_ = s.store.UpsertZone(ctx, st)
		res.Zones = append(res.Zones, st)
	}
	res.Entries = customEntries
	return res, nil
}

func (s *IntelService) rollback(ctx context.Context, zone, path string, backup []byte) {
	if len(backup) == 0 {
		return
	}
	if err := writeFileAtomic(path, backup); err != nil {
		log.Printf("[intel] 区 %s 回滚写盘失败：%v", zone, err)
		return
	}
	if err := s.ctl.AuthZoneReload(ctx, zone); err != nil {
		log.Printf("[intel] 区 %s 回滚热载失败：%v", zone, err)
	}
}

// Zones 返回最近一次编译的运行态快照（DB 持久化）。
func (s *IntelService) Zones(ctx context.Context) ([]IntelZoneState, error) {
	return s.store.ListZones(ctx)
}

// ListWhitelist 白名单列表。
func (s *IntelService) ListWhitelist(ctx context.Context) ([]IntelWhitelist, error) {
	return s.store.ListWhitelist(ctx)
}

// AddWhitelist 新增白名单 → 重编译 → 清该名负缓存（立即生效）。
func (s *IntelService) AddWhitelist(ctx context.Context, w IntelWhitelist) (IntelWhitelist, IntelCompileResult, error) {
	name := normalizeDomain(w.Value)
	if name == "" || strings.TrimSpace(w.Reason) == "" {
		return IntelWhitelist{}, IntelCompileResult{}, ErrWhitelistInvalid
	}
	if err := validateQnamePattern(name); err != nil {
		return IntelWhitelist{}, IntelCompileResult{}, fmt.Errorf("%w: %v", ErrWhitelistInvalid, err)
	}
	if w.Kind == "" {
		w.Kind = "qname"
	}
	if w.Kind != "qname" && w.Kind != "suffix" {
		return IntelWhitelist{}, IntelCompileResult{}, ErrWhitelistInvalid
	}
	if w.Source == "" {
		w.Source = "manual"
	}
	w.Value = name
	w.Enabled = true

	saved, err := s.store.CreateWhitelist(ctx, w)
	if err != nil {
		return IntelWhitelist{}, IntelCompileResult{}, err
	}
	res, cerr := s.Compile(ctx)
	if cerr != nil {
		log.Printf("[intel] 白名单已保存但编译失败: %v", cerr)
	}
	s.flush(ctx, name)
	return saved, res, nil
}

// DeleteWhitelist 删除白名单 → 重编译 → 清该名负缓存。
func (s *IntelService) DeleteWhitelist(ctx context.Context, id string) (IntelWhitelist, IntelCompileResult, error) {
	w, err := s.store.DeleteWhitelist(ctx, id)
	if err != nil {
		return IntelWhitelist{}, IntelCompileResult{}, err
	}
	res, cerr := s.Compile(ctx)
	if cerr != nil {
		log.Printf("[intel] 白名单已删除但编译失败: %v", cerr)
	}
	s.flush(ctx, w.Value)
	return w, res, nil
}

// flushCap 单区变更 flush 的条目上限。
// 逐条 flush 实测约 22ms；万级新增远超可接受耗时，超过上限则依赖缓存 TTL 自然过期。
const flushCap = 200

// flushChanges 对比编译前后 owner，清理新增/移除条目的缓存。
// before 为空（首轮/无旧文件）时不做全量 flush：unbound 刚加载区，缓存里没有旧答案。
func (s *IntelService) flushChanges(ctx context.Context, before, after string) {
	if strings.TrimSpace(before) == "" {
		return
	}
	added, removed := rpz.OwnerDiff(rpz.Owners(before), rpz.Owners(after))
	names := append(added, removed...)
	if len(names) == 0 {
		return
	}
	if len(names) > flushCap {
		log.Printf("[intel] 变更 %d 条超过 flush 上限 %d，依赖缓存 TTL 自然过期", len(names), flushCap)
		return
	}
	for _, n := range names {
		s.flush(ctx, n)
	}
}

// flush 清负缓存；失败只记日志（缓存最长 300s 自然过期）。
func (s *IntelService) flush(ctx context.Context, name string) {
	if strings.TrimSpace(name) == "" {
		return
	}
	if err := s.ctl.FlushZone(ctx, name); err != nil {
		log.Printf("[intel] flush_zone %s: %v", name, err)
	}
}

// ── 工具函数 ──

// normalizeDomain 去空白/尾点、转小写；保留最左侧 "*." 通配。
func normalizeDomain(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	v = strings.TrimSuffix(v, ".")
	if v == "" {
		return ""
	}
	return v
}

// whitelisted 判断域名是否被白名单覆盖（自身或任一上级后缀）。
func whitelisted(name string, set map[string]bool) bool {
	n := strings.TrimPrefix(name, "*.")
	if set[n] {
		return true
	}
	for {
		i := strings.Index(n, ".")
		if i < 0 {
			return false
		}
		n = n[i+1:]
		if set[n] {
			return true
		}
	}
}

// mapRPZAction 把条目动作映射为 RPZ 动作；未知动作按 nxdomain。
func mapRPZAction(action string) string {
	switch strings.TrimSpace(action) {
	case "drop":
		return rpz.ActionDrop
	case "tcp_only":
		return rpz.ActionTCPOnly
	case "redirect":
		return rpz.ActionRedirect
	default:
		return rpz.ActionNXDomain
	}
}

// nextSerial 生成单调递增 serial（同秒内编译则 +1）。
func nextSerial(prev uint32, now time.Time) uint32 {
	cur := uint32(now.Unix())
	if prev > 0 && cur <= prev {
		return prev + 1
	}
	return cur
}

func sha256Hex(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// writeFileAtomic 原子写盘（同目录 tmp + rename），避免 unbound 读到半截文件。
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
