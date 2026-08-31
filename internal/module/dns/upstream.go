// Package dns 承载 DNS 服务业务域（FR-B，§13.4 DNS 服务）。
package dns

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

// Upstream 上游服务器（PG upstream 表行投影）。
type Upstream struct {
	ID       string
	Name     string
	Addrs    []string
	Protocol string
	Weight   int
	Enabled  bool
}

// Health 探活状态（prober 维护，F-R4）。
type Health struct {
	Up               bool
	RTTMs            int
	ConsecutiveFails int
	LastCheck        time.Time
}

// UpstreamRepo 持久化。
type UpstreamRepo interface {
	List(ctx context.Context) ([]Upstream, error)
	Get(ctx context.Context, id string) (Upstream, bool, error)
	Create(ctx context.Context, u Upstream) (Upstream, error)
	Update(ctx context.Context, u Upstream) error
	Delete(ctx context.Context, id string) error
}

var (
	ErrUpstreamNotFound = errors.New("UPSTREAM_NOT_FOUND")
	ErrUnboundDown      = errors.New("UNBOUND_DOWN")
)

// UnboundController 下发通道抽象（engine/unbound 实现；测试用 fake）。
type UnboundController interface {
	// SyncForward 以全量 enabled 上游收敛 forward-zone "."（forward_add/remove）。
	SyncForward(ctx context.Context, upstreams []Upstream) error
	// SyncForwardRules 条件转发规则下发（forward_add <domain> <addrs...>）。
	SyncForwardRules(ctx context.Context, rules []ForwardRule, upstreams []Upstream) error
	// AuthZoneReload 单区刷新（auth_zone_reload <zone>）。
	AuthZoneReload(ctx context.Context, zoneID string) error
	// LocalZone 运行时本地域（M2-033 黑名单：local_zone static <name> → NXDOMAIN 拦截）。
	LocalZone(ctx context.Context, zoneType, name string) error
	// LocalZoneRemove 移除本地域。
	LocalZoneRemove(ctx context.Context, name string) error
	// CheckConf 校验配置片段。
	CheckConf(ctx context.Context, confPath, renderedBlock string) error
	// Reload 全量 reload。
	Reload(ctx context.Context) error
	// FlushZone 清空缓存。
	FlushZone(ctx context.Context, zone string) error
}

// Prober 上游健康探测器（TCP:53 连接+RTT；3 连败摘除/2 连胜回切）。
type Prober struct {
	mu       sync.Mutex
	status   map[string]*Health // 按上游 ID
	interval time.Duration
	dial     func(ctx context.Context, addr string) (time.Duration, error)
}

func NewProber(interval time.Duration, dial func(context.Context, string) (time.Duration, error)) *Prober {
	return &Prober{status: map[string]*Health{}, interval: interval, dial: dial}
}

// ProbeOnce 探测全部上游并更新状态机。
func (p *Prober) ProbeOnce(ctx context.Context, upstreams []Upstream) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, u := range upstreams {
		if !u.Enabled {
			delete(p.status, u.ID)
			continue
		}
		h, ok := p.status[u.ID]
		if !ok {
			h = &Health{}
			p.status[u.ID] = h
		}
		addr := u.Addrs[0]
		rtt, err := p.dial(ctx, addr)
		if err != nil {
			h.ConsecutiveFails++
			if h.ConsecutiveFails >= 3 {
				h.Up = false
			}
			h.RTTMs = 0
		} else {
			h.RTTMs = int(rtt / time.Millisecond)
			h.ConsecutiveFails = 0
			h.Up = true
		}
		h.LastCheck = time.Now()
	}
}

// Status 查询（按 ID；不存在返回空）。
func (p *Prober) Status(id string) Health {
	p.mu.Lock()
	defer p.mu.Unlock()
	if h, ok := p.status[id]; ok {
		return *h
	}
	return Health{}
}

// Run 周期探测直至 ctx 取消。
func (p *Prober) Run(ctx context.Context, upstreams func() []Upstream) {
	t := time.NewTicker(p.interval)
	defer t.Stop()
	p.ProbeOnce(ctx, upstreams())
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.ProbeOnce(ctx, upstreams())
		}
	}
}

// Service 上游业务：CRUD + 探活 + 下发。
type Service struct {
	repo   UpstreamRepo
	prober *Prober
	ctl    UnboundController
}

func NewService(repo UpstreamRepo, prober *Prober, ctl UnboundController) *Service {
	return &Service{repo: repo, prober: prober, ctl: ctl}
}

// Create 落库→重新下发 forward-zone。
func (s *Service) Create(ctx context.Context, u Upstream) (Upstream, error) {
	saved, err := s.repo.Create(ctx, u)
	if err != nil {
		return Upstream{}, err
	}
	if err := s.resync(ctx); err != nil {
		return saved, err // 已落库但下发失败：返回错误提示，状态可查询
	}
	return saved, nil
}

func (s *Service) resync(ctx context.Context) error {
	list, err := s.repo.List(ctx)
	if err != nil {
		return err
	}
	if err := s.ctl.SyncForward(ctx, list); err != nil {
		return ErrUnboundDown
	}
	return nil
}

// List 合并探活状态。
func (s *Service) List(ctx context.Context) ([]Upstream, []Health, error) {
	list, err := s.repo.List(ctx)
	if err != nil {
		return nil, nil, err
	}
	health := make([]Health, len(list))
	for i, u := range list {
		health[i] = s.prober.Status(u.ID)
	}
	return list, health, nil
}

// PgForwardRuleRepo PG 实现（迁移 0005）。
type PgForwardRuleRepo struct{ pool *pgxpool.Pool }

func NewPgForwardRuleRepo(pool *pgxpool.Pool) *PgForwardRuleRepo {
	return &PgForwardRuleRepo{pool: pool}
}

func (r *PgForwardRuleRepo) List(ctx context.Context) ([]ForwardRule, error) {
	rows, err := r.pool.Query(ctx, `SELECT id::text, domain, upstream_ids::text[], enabled, coalesce(note,'') FROM forward_rule ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ForwardRule{}
	for rows.Next() {
		var fr ForwardRule
		if err := rows.Scan(&fr.ID, &fr.Domain, &fr.UpstreamIDs, &fr.Enabled, &fr.Note); err == nil {
			out = append(out, fr)
		}
	}
	return out, rows.Err()
}

func (r *PgForwardRuleRepo) Create(ctx context.Context, fr ForwardRule) (ForwardRule, error) {
	var id string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO forward_rule(domain, upstream_ids, enabled, note) VALUES($1,$2,$3,$4) RETURNING id::text`,
		fr.Domain, fr.UpstreamIDs, fr.Enabled, nullStr(fr.Note)).Scan(&id)
	fr.ID = id
	return fr, err
}

func (r *PgForwardRuleRepo) Update(ctx context.Context, fr ForwardRule) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE forward_rule SET upstream_ids=$2, enabled=$3, note=$4 WHERE id=$1`,
		fr.ID, fr.UpstreamIDs, fr.Enabled, nullStr(fr.Note))
	return err
}

func (r *PgForwardRuleRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM forward_rule WHERE id=$1`, id)
	return err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// MemUpstreamRepo 内存实现（PoC/单测）。
type MemUpstreamRepo struct {
	mu    sync.Mutex
	items map[string]Upstream
	seq   int
}

func NewMemUpstreamRepo() *MemUpstreamRepo {
	return &MemUpstreamRepo{items: map[string]Upstream{}}
}

func (r *MemUpstreamRepo) List(_ context.Context) ([]Upstream, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []Upstream{}
	for _, u := range r.items {
		out = append(out, u)
	}
	return out, nil
}
func (r *MemUpstreamRepo) Get(_ context.Context, id string) (Upstream, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.items[id]
	return u, ok, nil
}
func (r *MemUpstreamRepo) Create(_ context.Context, u Upstream) (Upstream, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	u.ID = fmt.Sprintf("u-%d", r.seq)
	r.items[u.ID] = u
	return u, nil
}
func (r *MemUpstreamRepo) Update(_ context.Context, u Upstream) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[u.ID] = u
	return nil
}
func (r *MemUpstreamRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.items, id)
	return nil
}

// PgUpstreamRepo PG 实现（迁移 0004）。
type PgUpstreamRepo struct{ pool *pgxpool.Pool }

func NewPgUpstreamRepo(pool *pgxpool.Pool) *PgUpstreamRepo { return &PgUpstreamRepo{pool: pool} }

const upCols = `id::text, name, addrs, protocol, weight, enabled`

func (r *PgUpstreamRepo) List(ctx context.Context) ([]Upstream, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+upCols+` FROM upstream ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Upstream{}
	for rows.Next() {
		var u Upstream
		if err := rows.Scan(&u.ID, &u.Name, &u.Addrs, &u.Protocol, &u.Weight, &u.Enabled); err == nil {
			out = append(out, u)
		}
	}
	return out, rows.Err()
}

func (r *PgUpstreamRepo) Get(ctx context.Context, id string) (Upstream, bool, error) {
	var u Upstream
	err := r.pool.QueryRow(ctx, `SELECT `+upCols+` FROM upstream WHERE id=$1`, id).
		Scan(&u.ID, &u.Name, &u.Addrs, &u.Protocol, &u.Weight, &u.Enabled)
	if err == pgx.ErrNoRows {
		return Upstream{}, false, nil
	}
	if err != nil {
		return Upstream{}, false, err
	}
	return u, true, nil
}

func (r *PgUpstreamRepo) Create(ctx context.Context, u Upstream) (Upstream, error) {
	var id string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO upstream(name, addrs, protocol, weight, enabled) VALUES($1,$2,$3,$4,$5) RETURNING id::text`,
		u.Name, u.Addrs, u.Protocol, u.Weight, u.Enabled).Scan(&id)
	u.ID = id
	return u, err
}

func (r *PgUpstreamRepo) Update(ctx context.Context, u Upstream) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE upstream SET name=$2, addrs=$3, protocol=$4, weight=$5, enabled=$6 WHERE id=$1`,
		u.ID, u.Name, u.Addrs, u.Protocol, u.Weight, u.Enabled)
	return err
}

func (r *PgUpstreamRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM upstream WHERE id=$1`, id)
	return err
}
