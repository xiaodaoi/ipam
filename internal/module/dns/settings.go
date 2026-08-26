package dns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Settings DNS 行为参数（缓存+安全）。
type Settings struct {
	CacheMinTtl    int  `json:"cacheMinTtl"`
	CacheMaxTtl    int  `json:"cacheMaxTtl"`
	ServeExpired   bool `json:"serveExpired"`
	RrlEnabled     bool `json:"rrlEnabled"`
	RrlRate        int  `json:"rrlRate"`
	DnssecValidate bool `json:"dnssecValidate"`
	TcpOnly        bool `json:"tcpOnly,omitempty"`
}

// TtlOverride 每域名 TTL 覆盖（F-R3）。
type TtlOverride struct {
	Domain string
	TTL    int
}

// SettingsRepo 持久化。
type SettingsRepo interface {
	Get(ctx context.Context, key string) (json.RawMessage, bool, error)
	Set(ctx context.Context, key string, val json.RawMessage) error
	ListTtlOverrides(ctx context.Context) ([]TtlOverride, error)
	UpsertTtlOverride(ctx context.Context, o TtlOverride) error
}

var ErrInvalidConf = errors.New("INVALID_CONF")

// SettingsService 参数 CRUD：持久化→重渲染→checkconf→reload（校验失败不改运行态）。
type SettingsService struct {
	repo     SettingsRepo
	ctl      UnboundController
	confPath string
}

func NewSettingsService(repo SettingsRepo, ctl UnboundController, confPath string) *SettingsService {
	return &SettingsService{repo: repo, ctl: ctl, confPath: confPath}
}

// Get 读缓存+安全参数（缺省返回默认）。
func (s *SettingsService) Get(ctx context.Context) (Settings, error) {
	raw, ok, err := s.repo.Get(ctx, "cache")
	if err != nil {
		return Settings{}, err
	}
	if !ok {
		return DefaultSettings(), nil
	}
	var cache Settings
	if err := json.Unmarshal(raw, &cache); err != nil {
		return Settings{}, err
	}
	return cache, nil
}

// Update 保存→渲染→checkconf→reload；checkconf 失败回滚已保存值并返回 ErrInvalidConf。
func (s *SettingsService) Update(ctx context.Context, in Settings) error {
	old, _ := s.Get(ctx)
	raw, _ := json.Marshal(in)
	// 先渲染校验，成功才落库
	block := RenderSettingsBlock(in)
	if err := s.ctl.CheckConf(ctx, s.confPath, block); err != nil {
		return ErrInvalidConf
	}
	if err := s.repo.Set(ctx, "cache", raw); err != nil {
		return err
	}
	_ = s.ctl.Reload(ctx)
	_ = old
	return nil
}

// Flush 清空缓存（zone 缺省=全部）。
func (s *SettingsService) Flush(ctx context.Context, zone string) (flushed, cmd string, err error) {
	flushed = "all"
	if zone != "" {
		flushed = zone
	}
	cmd = "unbound-control flush"
	if zone != "" {
		cmd = "unbound-control flush_zone " + zone
	}
	if err := s.ctl.FlushZone(ctx, zone); err != nil {
		return "", "", err
	}
	return flushed, cmd, nil
}

// DefaultSettings 出厂默认（与 §8 预算一致）。
func DefaultSettings() Settings {
	return Settings{CacheMinTtl: 0, CacheMaxTtl: 86400, ServeExpired: false,
		RrlEnabled: true, RrlRate: 500, DnssecValidate: false, TcpOnly: false}
}

// RenderSettingsBlock 渲染 unbound.conf 参数段（server: 内语句）。
func RenderSettingsBlock(s Settings) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "cache-min-ttl: %d\n", s.CacheMinTtl)
	fmt.Fprintf(&sb, "cache-max-ttl: %d\n", s.CacheMaxTtl)
	fmt.Fprintf(&sb, "serve-expired: %s\n", boolYN(s.ServeExpired))
	fmt.Fprintf(&sb, "ratelimit: %s\n", boolYN(s.RrlEnabled))
	if s.RrlEnabled {
		fmt.Fprintf(&sb, "ratelimit-per-ip: %d\n", s.RrlRate)
	}
	fmt.Fprintf(&sb, "val-permissive-mode: %s\n", boolYN(!s.DnssecValidate))
	if s.TcpOnly {
		sb.WriteString("do-udp: no\n")
	}
	return sb.String()
}

func boolYN(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// MemSettingsRepo 内存实现。
type MemSettingsRepo struct {
	mu   sync.Mutex
	vals map[string]json.RawMessage
	ttls map[string]int
}

func NewMemSettingsRepo() *MemSettingsRepo {
	return &MemSettingsRepo{vals: map[string]json.RawMessage{}, ttls: map[string]int{}}
}

func (r *MemSettingsRepo) Get(_ context.Context, key string) (json.RawMessage, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.vals[key]
	return v, ok, nil
}
func (r *MemSettingsRepo) Set(_ context.Context, key string, val json.RawMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.vals[key] = val
	return nil
}
func (r *MemSettingsRepo) ListTtlOverrides(_ context.Context) ([]TtlOverride, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []TtlOverride{}
	for d, t := range r.ttls {
		out = append(out, TtlOverride{Domain: d, TTL: t})
	}
	return out, nil
}
func (r *MemSettingsRepo) UpsertTtlOverride(_ context.Context, o TtlOverride) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ttls[o.Domain] = o.TTL
	return nil
}

// PgSettingsRepo PG 实现（迁移 0008）。
type PgSettingsRepo struct{ pool *pgxpool.Pool }

func NewPgSettingsRepo(pool *pgxpool.Pool) *PgSettingsRepo { return &PgSettingsRepo{pool: pool} }

func (r *PgSettingsRepo) Get(ctx context.Context, key string) (json.RawMessage, bool, error) {
	var raw json.RawMessage
	err := r.pool.QueryRow(ctx, `SELECT value FROM dns_settings WHERE key=$1`, key).Scan(&raw)
	if err == pgx.ErrNoRows {
		return nil, false, nil
	}
	return raw, err == nil, err
}
func (r *PgSettingsRepo) Set(ctx context.Context, key string, val json.RawMessage) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO dns_settings(key, value) VALUES($1,$2)
		 ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, key, val)
	return err
}
func (r *PgSettingsRepo) ListTtlOverrides(ctx context.Context) ([]TtlOverride, error) {
	rows, err := r.pool.Query(ctx, `SELECT domain, ttl FROM dns_ttl_override ORDER BY domain`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TtlOverride{}
	for rows.Next() {
		var o TtlOverride
		if err := rows.Scan(&o.Domain, &o.TTL); err == nil {
			out = append(out, o)
		}
	}
	return out, rows.Err()
}
func (r *PgSettingsRepo) UpsertTtlOverride(ctx context.Context, o TtlOverride) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO dns_ttl_override(domain, ttl) VALUES($1,$2)
		 ON CONFLICT (domain) DO UPDATE SET ttl=EXCLUDED.ttl`, o.Domain, o.TTL)
	return err
}
