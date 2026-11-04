package dns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Blocklist 名单库（PG blocklist 行）。
type Blocklist struct {
	ID       string
	Name     string
	Kind     string // builtin | custom | feed
	SyncURL  string
	LastSync time.Time
	Version  int
}

// Entry 名单条目（PG blocklist_entry 行）。
type Entry struct {
	ListID         string
	TriggerType    string // qname | response_ip
	Pattern        string
	Action         string // nxdomain | drop | tcp_only | redirect
	RedirectTarget string
	Category       string
}

// PolicyGroup 策略分组（PG policy_group 行，view×网段×名单×时段）。
type PolicyGroup struct {
	ID       string
	Name     string
	ViewName string
	Cidrs    []string
	ListIDs  []string
	Schedule *Schedule
}

// Schedule 生效时段（M6-023：空=全天；Days 空=每天；Start>End 表示跨零点）。
type Schedule struct {
	Days  []int  `json:"days,omitempty"`
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

// Active 判定给定时刻是否处于生效窗口。schedule 为 nil 或字段为空时恒为真。
func (s *Schedule) Active(now time.Time) bool {
	if s == nil {
		return true
	}
	if len(s.Days) > 0 {
		ok := false
		for _, d := range s.Days {
			if d == int(now.Weekday()) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	start, end := parseClock(s.Start), parseClock(s.End)
	if start < 0 || end < 0 || start == end {
		return true
	}
	cur := now.Hour()*60 + now.Minute()
	if start < end {
		return cur >= start && cur < end
	}
	// 跨零点：22:00→06:00
	return cur >= start || cur < end
}

// parseClock "HH:MM" → 分钟数；空返回 0；非法返回 -1。
func parseClock(v string) int {
	if v == "" {
		return 0
	}
	parts := strings.SplitN(v, ":", 2)
	if len(parts) != 2 {
		return -1
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return -1
	}
	return h*60 + m
}

var (
	ErrBlocklistNotFound = errors.New("BLOCKLIST_NOT_FOUND")
	ErrFeedDown          = errors.New("FEED_DOWN")
	ErrViewNameDup       = errors.New("VIEW_NAME_DUP")
)

// BlocklistRepo 持久化。
// ErrBlocklistNotFound 名单/条目不存在（M2-024 删除语义）。

type BlocklistRepo interface {
	List(ctx context.Context) ([]Blocklist, error)
	Create(ctx context.Context, b Blocklist) (Blocklist, error)
	Get(ctx context.Context, id string) (Blocklist, bool, error)
	BumpVersion(ctx context.Context, id string, ts time.Time) error
	ListEntries(ctx context.Context, listID, q string) ([]Entry, error)
	UpsertEntries(ctx context.Context, entries []Entry) (int, error) // 返回实际新增数
	ListPolicyGroups(ctx context.Context) ([]PolicyGroup, error)
	CreatePolicyGroup(ctx context.Context, p PolicyGroup) (PolicyGroup, error)
	// EntriesForLists 聚合多名单条目（编译用）。
	EntriesForLists(ctx context.Context, listIDs []string) ([]Entry, error)
	// DeleteList 删除名单并级联删条目（M2-024）；不存在返回 ErrBlocklistNotFound。
	DeleteList(ctx context.Context, id string) error
	// DeleteEntry 按名单+pattern 自然键删条目（M2-024）；不存在返回 ErrBlocklistNotFound。
	DeleteEntry(ctx context.Context, listID, pattern string) error

	// ClearEntries 清空名单条目（CSV replace 导入，M6-023）；返回删除条数。
	ClearEntries(ctx context.Context, listID string) (int, error)
	// SnapshotVersion 保存当前条目快照（同版本幂等，M6-023）。
	SnapshotVersion(ctx context.Context, listID string) error
	// VersionList 版本快照（倒序）。
	VersionList(ctx context.Context, listID string) ([]VersionInfo, error)
	// RestoreVersion 用快照替换条目并递增版本，返回新版本号。
	RestoreVersion(ctx context.Context, listID string, version int) (int, error)
}

// VersionInfo 版本快照摘要。
type VersionInfo struct {
	Version   int
	Entries   int
	CreatedAt time.Time
}

// ErrVersionNotFound 版本快照不存在。
var ErrVersionNotFound = errors.New("VERSION_NOT_FOUND")

// FeedFetcher 订阅源拉取抽象（测试注入）。
type FeedFetcher func(ctx context.Context, url string) (string, error)

// feedHTTPClient 订阅源拉取专用（M5-032：net/http 默认 client 无超时，源挂死会无限泄漏 goroutine）。
var feedHTTPClient = &http.Client{Timeout: 30 * time.Second}

// HTTPSync 默认拉取器（30s 超时，正文 ≤5MB）。
func HTTPSync(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := feedHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("feed http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// BlocklistService 名单/分组/编译业务。
type BlocklistService struct {
	repo      BlocklistRepo
	fetch     FeedFetcher
	ctl       UnboundController
	rpzDir    string
	blockPage func(context.Context) string // redirect 提示页 IP 来源（M6-023）

	// 运行时收敛状态（M6-023 时段）：记录已下发的 local_zone/local_data，
	// 窗口切换时据此撤销不再生效的条目（unbound 运行时态无法枚举，需自行记账）。
	mu      sync.Mutex
	applied map[string]bool // "zone:<name>" / "data:<name>"
	now     func() time.Time
}

func NewBlocklistService(repo BlocklistRepo, fetch FeedFetcher, ctl UnboundController, rpzDir string) *BlocklistService {
	if fetch == nil {
		fetch = HTTPSync
	}
	return &BlocklistService{repo: repo, fetch: fetch, ctl: ctl, rpzDir: rpzDir,
		applied: map[string]bool{}, now: time.Now}
}

// SyncFeed 拉取→逐行解析→去重入库→版本+1；失败保留旧版（FEED_DOWN）。
func (s *BlocklistService) SyncFeed(ctx context.Context, listID string) (added int, total int, err error) {
	b, ok, err := s.repo.Get(ctx, listID)
	if err != nil || !ok {
		return 0, 0, ErrBlocklistNotFound
	}
	if b.Kind != "feed" {
		return 0, 0, fmt.Errorf("NOT_FEED: %s", b.Kind)
	}
	body, err := s.fetch(ctx, b.SyncURL)
	if err != nil {
		return 0, 0, ErrFeedDown
	}
	entries := ParseFeed(body, listID)
	// 同步前快照旧版本：订阅内容被覆盖后仍可回滚（M6-023）。
	_ = s.repo.SnapshotVersion(ctx, listID)
	added, err = s.repo.UpsertEntries(ctx, entries)
	if err != nil {
		return 0, 0, err
	}
	_ = s.repo.BumpVersion(ctx, listID, time.Now().UTC())
	existing, _ := s.repo.ListEntries(ctx, listID, "")
	return added, len(existing), nil
}

// ParseFeed 订阅正文逐行解析：跳过 # 注释与空行；保留通配 *.x 与纯域名；去重。
func ParseFeed(body, listID string) []Entry {
	seen := map[string]bool{}
	out := []Entry{}
	for _, line := range strings.Split(body, "\n") {
		p := strings.TrimSpace(line)
		if p == "" || strings.HasPrefix(p, "#") || strings.HasPrefix(p, "!") {
			continue
		}
		key := "qname|" + p
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Entry{ListID: listID, TriggerType: "qname", Pattern: p, Action: "nxdomain"})
	}
	return out
}

// SetBlockPageResolver 注入「拦截提示页 IP」来源（settings.cache.blockPageIP，M6-023）。
// 为空表示 redirect 条目退化为 NXDOMAIN（与 nxdomain 相同）。
func (s *BlocklistService) SetBlockPageResolver(fn func(context.Context) string) {
	s.blockPage = fn
}

// Compile 增量编译指定策略组的 RPZ zone（去重→zonefile→auth_zone_reload）。
func (s *BlocklistService) Compile(ctx context.Context, groupID string) (zone string, n int, path string, cmd string, err error) {
	groups, _ := s.repo.ListPolicyGroups(ctx)
	var g *PolicyGroup
	for i := range groups {
		if groups[i].ID == groupID {
			g = &groups[i]
		}
	}
	if g == nil {
		return "", 0, "", "", ErrPolicyNotFound
	}
	zone = g.ViewName + ".rpz"
	// 时段策略（M6-023）：窗口外不生效，由调度器在窗口切换时收敛。
	if !g.Schedule.Active(s.now()) {
		return zone, 0, "schedule inactive", "", nil
	}
	entries, err := s.repo.EntriesForLists(ctx, g.ListIDs)
	if err != nil {
		return "", 0, "", "", err
	}
	// M2-033：local_zone static 拦截（unbound 标准能力；RPZ auth-zone 路线在容器
	// unbound 1.26 编译下不可用——auth-zone 内 rpz: yes 实测 unknown keyword）。
	// pattern 映射：精确 → static <pattern>（仅该名）；*.d → static d（域及子域，语义略宽——记录于卡）。
	blockIP := ""
	if s.blockPage != nil {
		blockIP = s.blockPage(ctx)
	}
	cmds := make([]string, 0, len(entries))
	for _, e := range entries {
		raw := strings.TrimSpace(e.Pattern)
		name := strings.TrimSuffix(strings.TrimPrefix(raw, "*."), ".")
		if name == "" {
			continue
		}
		zt := "static"
		if e.Action == "redirect" && blockIP != "" && strings.HasPrefix(raw, "*.") {
			zt = "redirect"
		}
		if err := s.ensureZone(ctx, name, zt); err != nil {
			return zone, len(cmds), "local-zone (runtime)", cmd, err
		}
		cmds = append(cmds, "unbound-control local_zone "+name+" "+zt)
		// redirect：解析到提示页 IP（M6-023）。通配条目用 redirect 区，
		// 该区下所有名字（含子域）都答此 IP；local-data 只写区顶点。
		if e.Action == "redirect" && blockIP != "" {
			data := name + " 300 IN A " + blockIP
			if !s.isApplied("data:" + name) {
				if err := s.ctl.LocalData(ctx, data); err != nil {
					return zone, len(cmds), "local-data (runtime)", cmd, err
				}
				s.markApplied("data:" + name)
			}
			cmds = append(cmds, "unbound-control local_data "+data)
		}
	}
	cmd = strings.Join(cmds, "; ")
	return zone, len(cmds), "local-zone (runtime)", cmd, nil
}

// RemoveEntryZone 移除单条封禁的运行时 local_zone（M2-040：删除条目后解除拦截）。
func (s *BlocklistService) RemoveEntryZone(ctx context.Context, pattern string) error {
	name := strings.TrimPrefix(pattern, "*.")
	name = strings.TrimSuffix(name, ".")
	if name == "" {
		return nil
	}
	return s.ctl.LocalZoneRemove(ctx, name)
}

// ReplayAll 启动重放全部策略组的封禁 local_zone（M2-033：unbound 重启后运行时态恢复）。
func (s *BlocklistService) ReplayAll(ctx context.Context) error {
	_, err := s.Reconcile(ctx)
	return err
}

// ErrEmptyContent CSV 内容为空（400）。
var ErrEmptyContent = errors.New("EMPTY_CONTENT")

// ErrPackNotFound 内置分类包不存在。
var ErrPackNotFound = errors.New("BUILTIN_PACK_NOT_FOUND")

// ErrListNameDup 名单名重复（内置包安装用）。
var ErrListNameDup = errors.New("LIST_NAME_DUP")

// ImportCSV CSV 导入条目（M6-023）：校验→快照旧版→（可选清空）→入库→版本+1→收敛。
func (s *BlocklistService) ImportCSV(ctx context.Context, listID, content string, replace bool) (created int, skipped []string, total int, err error) {
	if _, ok, gerr := s.repo.Get(ctx, listID); gerr != nil {
		return 0, nil, 0, gerr
	} else if !ok {
		return 0, nil, 0, ErrBlocklistNotFound
	}
	entries, skipped := ParseEntryCSV(content)
	if len(entries) == 0 && len(skipped) == 0 {
		return 0, nil, 0, ErrEmptyContent
	}
	_ = s.repo.SnapshotVersion(ctx, listID)
	if replace {
		if _, err := s.repo.ClearEntries(ctx, listID); err != nil {
			return 0, nil, 0, err
		}
	}
	for i := range entries {
		entries[i].ListID = listID
	}
	added, err := s.repo.UpsertEntries(ctx, entries)
	if err != nil {
		return 0, skipped, 0, err
	}
	_ = s.repo.BumpVersion(ctx, listID, time.Now().UTC())
	all, _ := s.repo.ListEntries(ctx, listID, "")
	if rerr := s.ReplayAll(ctx); rerr != nil {
		log.Printf("[bl-import] replay: %v", rerr)
	}
	return added, skipped, len(all), nil
}

// InstallBuiltinPack 安装内置分类库为 builtin 名单（M6-023）。
func (s *BlocklistService) InstallBuiltinPack(ctx context.Context, key string) (Blocklist, error) {
	pack, ok := FindBuiltinPack(key)
	if !ok {
		return Blocklist{}, ErrPackNotFound
	}
	lists, err := s.repo.List(ctx)
	if err != nil {
		return Blocklist{}, err
	}
	for _, b := range lists {
		if b.Name == pack.Name && b.Kind == "builtin" {
			return Blocklist{}, ErrListNameDup
		}
	}
	saved, err := s.repo.Create(ctx, Blocklist{Name: pack.Name, Kind: "builtin"})
	if err != nil {
		return Blocklist{}, err
	}
	entries := make([]Entry, 0, len(pack.Patterns))
	for _, p := range pack.Patterns {
		entries = append(entries, Entry{
			ListID: saved.ID, TriggerType: "qname", Pattern: p, Action: "nxdomain",
			Category: strings.Join(pack.Categories, ","),
		})
	}
	if _, err := s.repo.UpsertEntries(ctx, entries); err != nil {
		return Blocklist{}, err
	}
	_ = s.repo.BumpVersion(ctx, saved.ID, time.Now().UTC())
	saved.Version = 2
	if rerr := s.ReplayAll(ctx); rerr != nil {
		log.Printf("[bl-builtin] replay: %v", rerr)
	}
	return saved, nil
}

// Rollback 回滚到指定版本快照（M6-023）。
func (s *BlocklistService) Rollback(ctx context.Context, listID string, version int) (Blocklist, error) {
	next, err := s.repo.RestoreVersion(ctx, listID, version)
	if err != nil {
		return Blocklist{}, err
	}
	b, ok, err := s.repo.Get(ctx, listID)
	if err != nil || !ok {
		return Blocklist{}, ErrBlocklistNotFound
	}
	b.Version = next
	if rerr := s.ReplayAll(ctx); rerr != nil {
		log.Printf("[bl-rollback] replay: %v", rerr)
	}
	return b, nil
}

// Reconcile 按时段收敛全部策略组：下发生效条目、撤销窗口外/已删除条目（M6-023）。
// 返回值=当前生效的条目数。由启动重放、条目变更与每分钟调度器共同调用。
func (s *BlocklistService) Reconcile(ctx context.Context) (int, error) {
	groups, err := s.repo.ListPolicyGroups(ctx)
	if err != nil {
		return 0, err
	}
	blockIP := ""
	if s.blockPage != nil {
		blockIP = s.blockPage(ctx)
	}
	now := s.now()
	// name → local-zone 类型（static|redirect）。
	// redirect 用于通配 redirect 条目：unbound 的 local-data 不展开 *.name，
	// 只有 redirect 区能把该域下所有名字都答成提示页 IP（M6-023 实测修正）。
	desiredZones := map[string]string{}
	desiredData := map[string]string{}
	for _, g := range groups {
		if !g.Schedule.Active(now) {
			continue
		}
		entries, err := s.repo.EntriesForLists(ctx, g.ListIDs)
		if err != nil {
			return 0, err
		}
		for _, e := range entries {
			raw := strings.TrimSpace(e.Pattern)
			name := strings.TrimSuffix(strings.TrimPrefix(raw, "*."), ".")
			if name == "" {
				continue
			}
			zt := "static"
			if e.Action == "redirect" && blockIP != "" {
				desiredData[name] = blockIP
				if strings.HasPrefix(raw, "*.") {
					zt = "redirect"
				}
			}
			// redirect 优先：同一名字既有通配 redirect 又有 NXDOMAIN 条目时给提示页。
			if cur, ok := desiredZones[name]; !ok || (cur != "redirect" && zt == "redirect") {
				desiredZones[name] = zt
			}
		}
	}

	// 撤销：先移除不再需要的 local_data，再移除 zone（顺序无关，但避免残留应答）。
	for key := range s.snapshot() {
		if strings.HasPrefix(key, "data:") {
			name := strings.TrimPrefix(key, "data:")
			if _, ok := desiredData[name]; !ok {
				if err := s.ctl.LocalDataRemove(ctx, name); err != nil {
					return 0, err
				}
				s.markRemoved(key)
			}
		}
	}
	for key := range s.snapshot() {
		if !strings.HasPrefix(key, "zone:") {
			continue
		}
		name, zt := parseZoneKey(key)
		if want, ok := desiredZones[name]; !ok || want != zt {
			if err := s.ctl.LocalZoneRemove(ctx, name); err != nil {
				return 0, err
			}
			s.markRemoved(key)
		}
	}

	// 下发：按名称排序保证行为确定（排障时可复现）。
	zoneNames := make([]string, 0, len(desiredZones))
	for name := range desiredZones {
		zoneNames = append(zoneNames, name)
	}
	sort.Strings(zoneNames)
	for _, name := range zoneNames {
		if err := s.ensureZone(ctx, name, desiredZones[name]); err != nil {
			return 0, err
		}
	}
	dataNames := make([]string, 0, len(desiredData))
	for name := range desiredData {
		dataNames = append(dataNames, name)
	}
	sort.Strings(dataNames)
	for _, name := range dataNames {
		if s.isApplied("data:" + name) {
			continue
		}
		data := name + " 300 IN A " + desiredData[name]
		if err := s.ctl.LocalData(ctx, data); err != nil {
			return 0, err
		}
		s.markApplied("data:" + name)
	}
	return len(desiredZones), nil
}

// parseZoneKey 拆解 applied 键 zone:<name>:<type>。
func parseZoneKey(key string) (name, zt string) {
	rest := strings.TrimPrefix(key, "zone:")
	if i := strings.LastIndex(rest, ":"); i > 0 {
		return rest[:i], rest[i+1:]
	}
	return rest, "static"
}

// ensureZone 保证 zone 以期望类型存在；类型变化时先删后加（unbound 不允许同区双类型）。
func (s *BlocklistService) ensureZone(ctx context.Context, name, zt string) error {
	want := "zone:" + name + ":" + zt
	if s.isApplied(want) {
		return nil
	}
	for key := range s.snapshot() {
		if !strings.HasPrefix(key, "zone:"+name+":") {
			continue
		}
		if err := s.ctl.LocalZoneRemove(ctx, name); err != nil {
			return err
		}
		s.markRemoved(key)
	}
	if err := s.ctl.LocalZone(ctx, name, zt); err != nil {
		return err
	}
	s.markApplied(want)
	return nil
}

func (s *BlocklistService) markApplied(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.applied == nil {
		s.applied = map[string]bool{}
	}
	s.applied[key] = true
}

func (s *BlocklistService) markRemoved(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.applied, key)
}

func (s *BlocklistService) isApplied(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applied[key]
}

func (s *BlocklistService) snapshot() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]bool, len(s.applied))
	for k := range s.applied {
		out[k] = true
	}
	return out
}

// StartScheduleScheduler 每分钟收敛一次时段策略（M6-023）。
// 无后台状态泄漏：仅一个 ticker，ctx 取消即退出。
func StartScheduleScheduler(ctx context.Context, svc *BlocklistService, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if _, err := svc.Reconcile(ctx); err != nil {
					log.Printf("[bl-schedule] reconcile: %v", err)
				}
			}
		}
	}()
}

var ErrPolicyNotFound = errors.New("POLICY_GROUP_NOT_FOUND")

// scheduleJSON 时段 → jsonb 文本（nil → NULL）。
func scheduleJSON(s *Schedule) any {
	if s == nil {
		return nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		return nil
	}
	return string(b)
}

// BuildRPZZone 去重后组装 RPZ zonefile 文本（action→RPZ 语义映射，§5.2）。
func BuildRPZZone(zone string, entries []Entry) (string, int) {
	var sb strings.Builder
	seen := map[string]bool{}
	n := 0
	for _, e := range entries {
		if e.TriggerType != "qname" {
			continue
		}
		p := strings.ToLower(e.Pattern)
		if seen[p] {
			continue
		}
		seen[p] = true
		rdata := "."
		switch e.Action {
		case "redirect":
			if e.RedirectTarget != "" {
				rdata = e.RedirectTarget
			} else {
				rdata = "block.corp.local."
			}
		case "drop", "tcp_only":
			rdata = "."
		}
		fmt.Fprintf(&sb, "%s CNAME %s\n", p, rdata)
		n++
	}
	return sb.String(), n
}

// MemBlocklistRepo 内存实现（PoC/单测）。
type MemBlocklistRepo struct {
	mu       sync.Mutex
	lists    map[string]Blocklist
	entries  []Entry
	groups   map[string]PolicyGroup
	versions map[string]map[int]memVersion
	lseq     int
	gseq     int
}

type memVersion struct {
	entries   []Entry
	createdAt time.Time
}

func NewMemBlocklistRepo() *MemBlocklistRepo {
	return &MemBlocklistRepo{lists: map[string]Blocklist{}, groups: map[string]PolicyGroup{},
		versions: map[string]map[int]memVersion{}}
}

func (r *MemBlocklistRepo) List(_ context.Context) ([]Blocklist, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []Blocklist{}
	for _, b := range r.lists {
		out = append(out, b)
	}
	return out, nil
}
func (r *MemBlocklistRepo) Create(_ context.Context, b Blocklist) (Blocklist, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lseq++
	b.ID = "bl-" + fmt.Sprint(r.lseq)
	b.Version = 1
	r.lists[b.ID] = b
	return b, nil
}
func (r *MemBlocklistRepo) Get(_ context.Context, id string) (Blocklist, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.lists[id]
	return b, ok, nil
}
func (r *MemBlocklistRepo) BumpVersion(_ context.Context, id string, ts time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.lists[id]; ok {
		b.Version++
		b.LastSync = ts
		r.lists[id] = b
	}
	return nil
}
func (r *MemBlocklistRepo) ListEntries(_ context.Context, listID, q string) ([]Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []Entry{}
	for _, e := range r.entries {
		if e.ListID != listID {
			continue
		}
		if q != "" && !strings.Contains(e.Pattern, q) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}
func (r *MemBlocklistRepo) UpsertEntries(_ context.Context, entries []Entry) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := map[string]bool{}
	for _, e := range r.entries {
		seen[e.ListID+"|"+e.Pattern] = true
	}
	added := 0
	for _, e := range entries {
		k := e.ListID + "|" + e.Pattern
		if seen[k] {
			continue
		}
		seen[k] = true
		r.entries = append(r.entries, e)
		added++
	}
	return added, nil
}
func (r *MemBlocklistRepo) ListPolicyGroups(_ context.Context) ([]PolicyGroup, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []PolicyGroup{}
	for _, g := range r.groups {
		out = append(out, g)
	}
	return out, nil
}
func (r *MemBlocklistRepo) CreatePolicyGroup(_ context.Context, g PolicyGroup) (PolicyGroup, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.groups {
		if e.ViewName == g.ViewName {
			return PolicyGroup{}, ErrViewNameDup
		}
	}
	r.gseq++
	g.ID = "pg-" + fmt.Sprint(r.gseq)
	r.groups[g.ID] = g
	return g, nil
}
func (r *MemBlocklistRepo) EntriesForLists(_ context.Context, listIDs []string) ([]Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	want := map[string]bool{}
	for _, id := range listIDs {
		want[id] = true
	}
	out := []Entry{}
	for _, e := range r.entries {
		if want[e.ListID] {
			out = append(out, e)
		}
	}
	return out, nil
}

// PgBlocklistRepo PG 实现（迁移 0001 + 0007 索引）。
type PgBlocklistRepo struct{ pool *pgxpool.Pool }

func (r *MemBlocklistRepo) DeleteList(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.lists[id]; !ok {
		return ErrBlocklistNotFound
	}
	delete(r.lists, id)
	kept := r.entries[:0]
	for _, e := range r.entries {
		if e.ListID != id {
			kept = append(kept, e)
		}
	}
	r.entries = kept
	return nil
}

func (r *MemBlocklistRepo) DeleteEntry(_ context.Context, listID, pattern string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	kept := r.entries[:0]
	found := false
	for _, e := range r.entries {
		if e.ListID == listID && e.Pattern == pattern {
			found = true
			continue
		}
		kept = append(kept, e)
	}
	r.entries = kept
	if !found {
		return ErrBlocklistNotFound
	}
	return nil
}

func NewPgBlocklistRepo(pool *pgxpool.Pool) *PgBlocklistRepo { return &PgBlocklistRepo{pool: pool} }

func (r *PgBlocklistRepo) DeleteList(ctx context.Context, id string) error {
	if _, err := r.pool.Exec(ctx, `DELETE FROM blocklist_entry WHERE list_id = $1`, id); err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `DELETE FROM blocklist WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrBlocklistNotFound
	}
	return nil
}

func (r *PgBlocklistRepo) DeleteEntry(ctx context.Context, listID, pattern string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM blocklist_entry WHERE list_id = $1 AND pattern = $2`, listID, pattern)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrBlocklistNotFound
	}
	return nil
}

func (r *PgBlocklistRepo) List(ctx context.Context) ([]Blocklist, error) {
	rows, err := r.pool.Query(ctx, `SELECT id::text, name, kind, coalesce(sync_url,''), coalesce(last_sync,'1970-01-01'::timestamptz), version FROM blocklist ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Blocklist{}
	for rows.Next() {
		var b Blocklist
		if err := rows.Scan(&b.ID, &b.Name, &b.Kind, &b.SyncURL, &b.LastSync, &b.Version); err == nil {
			out = append(out, b)
		}
	}
	return out, rows.Err()
}
func (r *PgBlocklistRepo) Create(ctx context.Context, b Blocklist) (Blocklist, error) {
	var id string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO blocklist(name, kind, sync_url) VALUES($1,$2,$3) RETURNING id::text`,
		b.Name, b.Kind, nullStr(b.SyncURL)).Scan(&id)
	b.ID, b.Version = id, 1
	return b, err
}
func (r *PgBlocklistRepo) Get(ctx context.Context, id string) (Blocklist, bool, error) {
	var b Blocklist
	err := r.pool.QueryRow(ctx,
		`SELECT id::text, name, kind, coalesce(sync_url,''), coalesce(last_sync,'1970-01-01'::timestamptz), version FROM blocklist WHERE id=$1`, id).
		Scan(&b.ID, &b.Name, &b.Kind, &b.SyncURL, &b.LastSync, &b.Version)
	if err == pgx.ErrNoRows {
		return Blocklist{}, false, nil
	}
	return b, err == nil, err
}
func (r *PgBlocklistRepo) BumpVersion(ctx context.Context, id string, ts time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE blocklist SET version=version+1, last_sync=$2 WHERE id=$1`, id, ts)
	return err
}
func (r *PgBlocklistRepo) ListEntries(ctx context.Context, listID, q string) ([]Entry, error) {
	sql := `SELECT list_id::text, trigger_type, pattern, action, coalesce(redirect_target,''), coalesce(category,'') FROM blocklist_entry WHERE list_id=$1`
	args := []any{listID}
	if q != "" {
		args = append(args, "%"+q+"%")
		sql += ` AND pattern ILIKE $2`
	}
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ListID, &e.TriggerType, &e.Pattern, &e.Action, &e.RedirectTarget, &e.Category); err == nil {
			out = append(out, e)
		}
	}
	return out, rows.Err()
}
func (r *PgBlocklistRepo) UpsertEntries(ctx context.Context, entries []Entry) (int, error) {
	added := 0
	for _, e := range entries {
		tag, err := r.pool.Exec(ctx,
			`INSERT INTO blocklist_entry(list_id, trigger_type, pattern, action, redirect_target, category)
			 VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT (list_id,trigger_type,pattern) DO NOTHING`,
			e.ListID, e.TriggerType, e.Pattern, e.Action, nullStr(e.RedirectTarget), nullStr(e.Category))
		if err != nil {
			return added, err
		}
		if tag.RowsAffected() > 0 {
			added++
		}
	}
	return added, nil
}
func (r *PgBlocklistRepo) ListPolicyGroups(ctx context.Context) ([]PolicyGroup, error) {
	rows, err := r.pool.Query(ctx, `SELECT id::text, name, view_name, cidrs::text[], list_ids::text[], coalesce(schedule,'null'::jsonb)::text FROM policy_group`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PolicyGroup{}
	for rows.Next() {
		var g PolicyGroup
		var rawSchedule string
		if err := rows.Scan(&g.ID, &g.Name, &g.ViewName, &g.Cidrs, &g.ListIDs, &rawSchedule); err == nil {
			_ = json.Unmarshal([]byte(rawSchedule), &g.Schedule)
			out = append(out, g)
		}
	}
	return out, rows.Err()
}
func (r *PgBlocklistRepo) CreatePolicyGroup(ctx context.Context, g PolicyGroup) (PolicyGroup, error) {
	var id string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO policy_group(name, view_name, cidrs, list_ids, schedule) VALUES($1,$2,$3,$4,$5::jsonb) RETURNING id::text`,
		g.Name, g.ViewName, g.Cidrs, g.ListIDs, scheduleJSON(g.Schedule)).Scan(&id)
	g.ID = id
	return g, err
}
func (r *PgBlocklistRepo) EntriesForLists(ctx context.Context, listIDs []string) ([]Entry, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT list_id::text, trigger_type, pattern, action, coalesce(redirect_target,''), coalesce(category,'') FROM blocklist_entry WHERE list_id = ANY($1)`,
		listIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ListID, &e.TriggerType, &e.Pattern, &e.Action, &e.RedirectTarget, &e.Category); err == nil {
			out = append(out, e)
		}
	}
	return out, rows.Err()
}

// MatchPolicyView 模拟来源 → view 匹配（M2-031）：遍历策略分组 CIDR，首中返回 viewName。
func MatchPolicyView(groups []PolicyGroup, clientIP string) (string, bool) {
	ip, err := netip.ParseAddr(clientIP)
	if err != nil {
		return "", false
	}
	for _, g := range groups {
		for _, c := range g.Cidrs {
			if p, err := netip.ParsePrefix(c); err == nil && p.Contains(ip) {
				return g.ViewName, true
			}
		}
	}
	return "", false
}

// ── 版本快照（M6-023：内置库/订阅同步/CSV 导入的可回滚基线）──

func (r *MemBlocklistRepo) ClearEntries(_ context.Context, listID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	kept := r.entries[:0]
	n := 0
	for _, e := range r.entries {
		if e.ListID == listID {
			n++
			continue
		}
		kept = append(kept, e)
	}
	r.entries = kept
	return n, nil
}

func (r *PgBlocklistRepo) ClearEntries(ctx context.Context, listID string) (int, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM blocklist_entry WHERE list_id=$1`, listID)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (r *MemBlocklistRepo) SnapshotVersion(_ context.Context, listID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.lists[listID]
	if !ok {
		return ErrBlocklistNotFound
	}
	if r.versions[listID] == nil {
		r.versions[listID] = map[int]memVersion{}
	}
	if _, exists := r.versions[listID][b.Version]; exists {
		return nil
	}
	var snap []Entry
	seen := map[string]bool{}
	for _, e := range r.entries {
		if e.ListID != listID || seen[e.TriggerType+"|"+e.Pattern] {
			continue
		}
		seen[e.TriggerType+"|"+e.Pattern] = true
		snap = append(snap, e)
	}
	r.versions[listID][b.Version] = memVersion{entries: snap, createdAt: time.Now().UTC()}
	return nil
}

func (r *MemBlocklistRepo) VersionList(_ context.Context, listID string) ([]VersionInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []VersionInfo{}
	for v, snap := range r.versions[listID] {
		out = append(out, VersionInfo{Version: v, Entries: len(snap.entries), CreatedAt: snap.createdAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version > out[j].Version })
	return out, nil
}

func (r *MemBlocklistRepo) RestoreVersion(_ context.Context, listID string, version int) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snap, ok := r.versions[listID][version]
	if !ok {
		return 0, ErrVersionNotFound
	}
	kept := r.entries[:0]
	for _, e := range r.entries {
		if e.ListID != listID {
			kept = append(kept, e)
		}
	}
	r.entries = append(kept, snap.entries...)
	b := r.lists[listID]
	b.Version++
	r.lists[listID] = b
	return b.Version, nil
}

func (r *PgBlocklistRepo) SnapshotVersion(ctx context.Context, listID string) error {
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO blocklist_version(list_id, version, entries, created_at)
		SELECT b.id, b.version,
		       coalesce((SELECT jsonb_agg(jsonb_build_object(
		           'trigger_type', e.trigger_type, 'pattern', e.pattern, 'action', e.action,
		           'redirect_target', coalesce(e.redirect_target,''), 'category', coalesce(e.category,''))
		         ) FROM blocklist_entry e WHERE e.list_id = b.id), '[]'::jsonb),
		       now()
		FROM blocklist b WHERE b.id = $1
		ON CONFLICT (list_id, version) DO NOTHING`, listID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// ON CONFLICT 或名单不存在：区分一下，避免静默吞错
		var exists bool
		if err := r.pool.QueryRow(ctx, `SELECT true FROM blocklist WHERE id=$1`, listID).Scan(&exists); err != nil {
			return ErrBlocklistNotFound
		}
	}
	return nil
}

func (r *PgBlocklistRepo) VersionList(ctx context.Context, listID string) ([]VersionInfo, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT version, jsonb_array_length(entries), created_at FROM blocklist_version
		 WHERE list_id=$1 ORDER BY version DESC`, listID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []VersionInfo{}
	for rows.Next() {
		var v VersionInfo
		if err := rows.Scan(&v.Version, &v.Entries, &v.CreatedAt); err == nil {
			out = append(out, v)
		}
	}
	return out, rows.Err()
}

func (r *PgBlocklistRepo) RestoreVersion(ctx context.Context, listID string, version int) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var entriesJSON []byte
	if err := tx.QueryRow(ctx, `SELECT entries FROM blocklist_version WHERE list_id=$1 AND version=$2`,
		listID, version).Scan(&entriesJSON); err != nil {
		if err == pgx.ErrNoRows {
			return 0, ErrVersionNotFound
		}
		return 0, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM blocklist_entry WHERE list_id=$1`, listID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO blocklist_entry(list_id, trigger_type, pattern, action, redirect_target, category)
		SELECT $1, x.trigger_type, x.pattern, x.action, nullif(x.redirect_target,''), nullif(x.category,'')
		FROM jsonb_to_recordset($2::jsonb) AS x(
			trigger_type text, pattern text, action text, redirect_target text, category text)`,
		listID, string(entriesJSON)); err != nil {
		return 0, err
	}
	var next int
	if err := tx.QueryRow(ctx, `UPDATE blocklist SET version=version+1 WHERE id=$1 RETURNING version`,
		listID).Scan(&next); err != nil {
		if err == pgx.ErrNoRows {
			return 0, ErrBlocklistNotFound
		}
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return next, nil
}
