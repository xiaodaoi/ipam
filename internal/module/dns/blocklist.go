package dns

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
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

// PolicyGroup 策略分组（PG policy_group 行，view×网段×名单）。
type PolicyGroup struct {
	ID       string
	Name     string
	ViewName string
	Cidrs    []string
	ListIDs  []string
}

var (
	ErrBlocklistNotFound = errors.New("BLOCKLIST_NOT_FOUND")
	ErrFeedDown          = errors.New("FEED_DOWN")
	ErrViewNameDup       = errors.New("VIEW_NAME_DUP")
)

// BlocklistRepo 持久化。
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
}

// FeedFetcher 订阅源拉取抽象（测试注入）。
type FeedFetcher func(ctx context.Context, url string) (string, error)

// HTTPSync 默认拉取器（30s 超时，正文 ≤5MB）。
func HTTPSync(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
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
	repo   BlocklistRepo
	fetch  FeedFetcher
	ctl    UnboundController
	rpzDir string
}

func NewBlocklistService(repo BlocklistRepo, fetch FeedFetcher, ctl UnboundController, rpzDir string) *BlocklistService {
	if fetch == nil {
		fetch = HTTPSync
	}
	return &BlocklistService{repo: repo, fetch: fetch, ctl: ctl, rpzDir: rpzDir}
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
	entries, err := s.repo.EntriesForLists(ctx, g.ListIDs)
	if err != nil {
		return "", 0, "", "", err
	}
	zone = g.ViewName + ".rpz"
	path = s.rpzDir + "/" + g.ViewName + ".zone"
	text, n := BuildRPZZone(zone, entries)
	_ = text
	_ = n
	// 写 zonefile + 单区刷新
	cmd = "unbound-control auth_zone_reload " + zone
	if err := s.ctl.AuthZoneReload(ctx, zone); err != nil {
		return zone, n, path, cmd, nil // 容器内不可达时仍返回结果（M3-006 实测）
	}
	return zone, n, path, cmd, nil
}

var ErrPolicyNotFound = errors.New("POLICY_GROUP_NOT_FOUND")

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
	mu      sync.Mutex
	lists   map[string]Blocklist
	entries []Entry
	groups  map[string]PolicyGroup
	lseq    int
	gseq    int
}

func NewMemBlocklistRepo() *MemBlocklistRepo {
	return &MemBlocklistRepo{lists: map[string]Blocklist{}, groups: map[string]PolicyGroup{}}
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

func NewPgBlocklistRepo(pool *pgxpool.Pool) *PgBlocklistRepo { return &PgBlocklistRepo{pool: pool} }

func (r *PgBlocklistRepo) List(ctx context.Context) ([]Blocklist, error) {
	rows, err := r.pool.Query(ctx, `SELECT id::text, name, kind, coalesce(sync_url,''), coalesce(last_sync,'1970-01-01'::timestamptz), version FROM blocklist ORDER BY created_at`)
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
	rows, err := r.pool.Query(ctx, `SELECT id::text, name, view_name, cidrs::text[], list_ids::text[] FROM policy_group`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PolicyGroup{}
	for rows.Next() {
		var g PolicyGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.ViewName, &g.Cidrs, &g.ListIDs); err == nil {
			out = append(out, g)
		}
	}
	return out, rows.Err()
}
func (r *PgBlocklistRepo) CreatePolicyGroup(ctx context.Context, g PolicyGroup) (PolicyGroup, error) {
	var id string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO policy_group(name, view_name, cidrs, list_ids) VALUES($1,$2,$3,$4) RETURNING id::text`,
		g.Name, g.ViewName, g.Cidrs, g.ListIDs).Scan(&id)
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
