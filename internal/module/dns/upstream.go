// Package dns 承载 DNS 服务业务域（FR-B，§13.4 DNS 服务）。
package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

// Upstream 上游服务器（PG upstream 表行投影）。
type Upstream struct {
	ID        string
	Name      string
	Addrs     []string
	Protocol  string
	TLSName   string // DoT SNI/证书名（protocol=dot，M6-023）
	Weight    int
	IsDefault bool // 默认公网出口：多个 enabled+IsDefault 共同下发 forward-zone "."（M6-041）
	Enabled   bool
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
	// ErrDefaultUpstreamNotPublic 默认公网出口只能使用公网地址（M6-041）：
	// "." 必须保持为公网回落，内网 DNS 只能通过条件转发规则参与。
	ErrDefaultUpstreamNotPublic = errors.New("DEFAULT_UPSTREAM_NOT_PUBLIC")
)

// DefaultForwardUpstreams 返回参与 forward-zone "." 的上游集合（M6-041）：
//   - 一旦存在任何 is_default 标记，只使用 enabled 的默认上游；默认全部停用时返回空，
//     绝不回落到非默认上游——否则可能把内网 DNS 带入 "."（M3-011 / M6-042）。
//   - 从未标记默认的旧部署回退第一条 enabled（兼容 0050 之前的行为）。
func DefaultForwardUpstreams(list []Upstream) []Upstream {
	picked := make([]Upstream, 0, len(list))
	hasDefault := false
	for _, u := range list {
		if !u.IsDefault {
			continue
		}
		hasDefault = true
		if u.Enabled {
			picked = append(picked, u)
		}
	}
	if hasDefault {
		return picked
	}
	for _, u := range list {
		if u.Enabled {
			return []Upstream{u}
		}
	}
	return nil
}

// ValidateDefaultUpstream 校验默认公网出口：所有地址必须是公网 IP。
// 允许 IPv4/IPv6，拒绝 RFC1918、环回、链路本地、ULA、CGNAT 等私网/保留地址。
func ValidateDefaultUpstream(u Upstream) error {
	if !u.IsDefault {
		return nil
	}
	for _, raw := range u.Addrs {
		host := hostOnly(raw)
		ip, err := netip.ParseAddr(host)
		if err != nil {
			return fmt.Errorf("%w: 地址 %q 不是合法 IP", ErrDefaultUpstreamNotPublic, raw)
		}
		if !isPublicIP(ip) {
			return fmt.Errorf("%w: 地址 %q 属于内网/保留地址", ErrDefaultUpstreamNotPublic, raw)
		}
	}
	return nil
}

// hostOnly 从 ip[:port] / [v6][:port] 中取主机部分。
func hostOnly(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// reservedPrefixes 非公网地址段（除 netip 已覆盖的 RFC1918/ULA/环回/链路本地）。
var reservedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"), // CGNAT
	netip.MustParsePrefix("192.0.0.0/24"),  // IETF 协议保留
	netip.MustParsePrefix("198.18.0.0/15"), // 基准测试
	netip.MustParsePrefix("240.0.0.0/4"),   // 保留（含广播）
	netip.MustParsePrefix("2001:db8::/32"), // 文档示例
	netip.MustParsePrefix("64:ff9b::/96"),  // NAT64 内嵌 IPv4，视作非公网直连
	netip.MustParsePrefix("2002::/16"),     // 6to4（可承载内网 IPv4）
}

// isPublicIP 判断地址能否作为公网递归出口。
func isPublicIP(ip netip.Addr) bool {
	if ip.Is4In6() {
		ip = ip.Unmap()
	}
	if !ip.IsValid() || ip.IsUnspecified() || ip.IsLoopback() ||
		ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	for _, p := range reservedPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

// UnboundController 下发通道抽象（engine/unbound 实现；测试用 fake）。
type UnboundController interface {
	// SyncForward 以全量 enabled 上游收敛 forward-zone "."（forward_add/remove）。
	SyncForward(ctx context.Context, upstreams []Upstream) error
	// SyncForwardRules 条件转发规则下发（forward_add <domain> <addrs...>）。
	SyncForwardRules(ctx context.Context, rules []ForwardRule, upstreams []Upstream) error
	// AuthZoneReload 单区刷新（auth_zone_reload <zone>）。
	AuthZoneReload(ctx context.Context, zoneID string) error
	// AuthZoneSerials 查询运行态 auth-zone/RPZ 区 serial（M6-038 实际态校正）。
	AuthZoneSerials(ctx context.Context) (map[string]uint32, error)
	// LocalZone 运行时本地域（M2-033 黑名单：local_zone static <name> → NXDOMAIN 拦截）。
	LocalZone(ctx context.Context, name, zoneType string) error
	// LocalZoneRemove 移除本地域。
	LocalZoneRemove(ctx context.Context, name string) error
	// LocalData 运行时本地记录（M6-023：redirect 封禁指向提示页 IP）。
	LocalData(ctx context.Context, data string) error
	// LocalDataRemove 移除本地记录。
	LocalDataRemove(ctx context.Context, name string) error
	// CheckConf 校验配置片段。
	CheckConf(ctx context.Context, confPath, renderedBlock string) error
	// Reload 全量 reload。
	Reload(ctx context.Context) error
	// FlushZone 清空缓存。
	FlushZone(ctx context.Context, zone string) error
}

// maxConsecutiveFails 连续失败计数上限（避免界面出现 6 万+ 的无意义数字）。
const maxConsecutiveFails = 999

// Prober 上游健康探测器（TCP:53 连接+RTT；连续 3 次失败标记异常，一次成功恢复）。
// 探测结果仅用于界面展示，不参与转发选路——SyncForward/SyncForwardRules 只按 enabled 过滤。
type Prober struct {
	mu       sync.Mutex
	status   map[string]*Health // 按上游 ID
	interval time.Duration
	dial     func(ctx context.Context, addr string) (time.Duration, error)
}

func NewProber(interval time.Duration, dial func(context.Context, string) (time.Duration, error)) *Prober {
	return &Prober{status: map[string]*Health{}, interval: interval, dial: dial}
}

type probeResult struct {
	id       string
	disabled bool
	rtt      time.Duration
	ok       bool
}

// ProbeOnce 探测全部上游并更新状态机。
// 网络探测在锁外完成，避免某个地址 2s 超时阻塞 Status 查询（上游列表接口）。
// 一个上游配置多个地址时任一可达即视为在线，RTT 取成功地址中的最小值。
func (p *Prober) ProbeOnce(ctx context.Context, upstreams []Upstream) {
	results := make([]probeResult, 0, len(upstreams))
	for _, u := range upstreams {
		if !u.Enabled {
			results = append(results, probeResult{id: u.ID, disabled: true})
			continue
		}
		res := probeResult{id: u.ID}
		for _, addr := range u.Addrs {
			rtt, err := p.dial(ctx, addr)
			if err != nil {
				continue
			}
			if !res.ok || rtt < res.rtt {
				res.rtt = rtt
			}
			res.ok = true
		}
		results = append(results, res)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	for _, res := range results {
		if res.disabled {
			delete(p.status, res.id)
			continue
		}
		h, ok := p.status[res.id]
		if !ok {
			h = &Health{}
			p.status[res.id] = h
		}
		if res.ok {
			h.RTTMs = int(res.rtt / time.Millisecond)
			h.ConsecutiveFails = 0
			h.Up = true
		} else {
			h.ConsecutiveFails++
			if h.ConsecutiveFails >= 3 {
				h.Up = false
			}
			if h.ConsecutiveFails > maxConsecutiveFails {
				h.ConsecutiveFails = maxConsecutiveFails
			}
			h.RTTMs = 0
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
	// RulesSync 规则区联动刷新钩子（上游 addrs/enabled 变更影响引用它的规则区）
	RulesSync func(ctx context.Context) error
}

func NewService(repo UpstreamRepo, prober *Prober, ctl UnboundController) *Service {
	return &Service{repo: repo, prober: prober, ctl: ctl}
}

// Create 落库→重新下发 forward-zone。
func (s *Service) Create(ctx context.Context, u Upstream) (Upstream, error) {
	if err := ValidateDefaultUpstream(u); err != nil {
		return Upstream{}, err
	}
	saved, err := s.repo.Create(ctx, u)
	if err != nil {
		return Upstream{}, err
	}
	if err := s.resync(ctx); err != nil {
		return saved, err // 已落库但下发失败：返回错误提示，状态可查询
	}
	return saved, nil
}

// Update 更新上游（校验默认出口地址后落库并重新下发）。
func (s *Service) Update(ctx context.Context, u Upstream) error {
	if err := ValidateDefaultUpstream(u); err != nil {
		return err
	}
	if err := s.repo.Update(ctx, u); err != nil {
		return err
	}
	return s.resync(ctx)
}

func (s *Service) resync(ctx context.Context) error {
	list, err := s.repo.List(ctx)
	if err != nil {
		return err
	}
	if err := s.ctl.SyncForward(ctx, list); err != nil {
		return ErrUnboundDown
	}
	if s.RulesSync != nil {
		if err := s.RulesSync(ctx); err != nil {
			return ErrUnboundDown
		}
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

const upCols = `id::text, name, addrs, protocol, coalesce(tls_name,''), weight, is_default, enabled`

func (r *PgUpstreamRepo) List(ctx context.Context) ([]Upstream, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+upCols+` FROM upstream ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Upstream{}
	for rows.Next() {
		var u Upstream
		if err := rows.Scan(&u.ID, &u.Name, &u.Addrs, &u.Protocol, &u.TLSName, &u.Weight, &u.IsDefault, &u.Enabled); err == nil {
			out = append(out, u)
		}
	}
	return out, rows.Err()
}

func (r *PgUpstreamRepo) Get(ctx context.Context, id string) (Upstream, bool, error) {
	var u Upstream
	err := r.pool.QueryRow(ctx, `SELECT `+upCols+` FROM upstream WHERE id=$1`, id).
		Scan(&u.ID, &u.Name, &u.Addrs, &u.Protocol, &u.TLSName, &u.Weight, &u.IsDefault, &u.Enabled)
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
		`INSERT INTO upstream(name, addrs, protocol, tls_name, weight, is_default, enabled) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id::text`,
		u.Name, u.Addrs, u.Protocol, u.TLSName, u.Weight, u.IsDefault, u.Enabled).Scan(&id)
	u.ID = id
	return u, err
}

func (r *PgUpstreamRepo) Update(ctx context.Context, u Upstream) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE upstream SET name=$2, addrs=$3, protocol=$4, tls_name=$5, weight=$6, is_default=$7, enabled=$8 WHERE id=$1`,
		u.ID, u.Name, u.Addrs, u.Protocol, u.TLSName, u.Weight, u.IsDefault, u.Enabled)
	return err
}

func (r *PgUpstreamRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM upstream WHERE id=$1`, id)
	return err
}
