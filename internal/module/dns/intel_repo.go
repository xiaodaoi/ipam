package dns

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// 白名单/运行态持久化（M6-038 P0）。
//
// 大规模 IOC 不入库：只存元数据与白名单；zone 内容以磁盘编译产物为准。

var (
	// ErrWhitelistNotFound 白名单条目不存在。
	ErrWhitelistNotFound = errors.New("WHITELIST_NOT_FOUND")
	// ErrWhitelistDup 同一 kind+value 重复。
	ErrWhitelistDup = errors.New("WHITELIST_DUP")
	// ErrWhitelistInvalid 白名单条目非法（空值/缺原因）。
	ErrWhitelistInvalid = errors.New("WHITELIST_INVALID")
)

// IntelWhitelist 白名单条目（passthru，必须排在最前）。
type IntelWhitelist struct {
	ID        string
	Kind      string // qname（仅该名） | suffix（该名及子域）
	Value     string
	Reason    string
	Category  string
	Source    string // manual | builtin | feed_exclude | auto_fp
	ExpiresAt *time.Time
	Enabled   bool
	CreatedBy string
	CreatedAt time.Time
}

// Active 判断条目在给定时刻是否生效（过期即失效）。
func (w IntelWhitelist) Active(now time.Time) bool {
	if !w.Enabled {
		return false
	}
	return w.ExpiresAt == nil || w.ExpiresAt.After(now)
}

// IntelZoneState 编译产物与运行态快照。
type IntelZoneState struct {
	Zone         string
	Entries      int
	Serial       uint32
	LoadedSerial uint32
	SHA          string
	Path         string
	Status       string // empty | loaded | stale | failed
	UpdatedAt    time.Time
}

// IntelRepo 白名单 + zone 运行态。
type IntelRepo interface {
	ListWhitelist(ctx context.Context) ([]IntelWhitelist, error)
	CreateWhitelist(ctx context.Context, w IntelWhitelist) (IntelWhitelist, error)
	DeleteWhitelist(ctx context.Context, id string) (IntelWhitelist, error)
	ListZones(ctx context.Context) ([]IntelZoneState, error)
	UpsertZone(ctx context.Context, z IntelZoneState) error
}

// ── 内存实现（无 DB 的开发/单测路径）──

// MemIntelRepo 内存实现。
type MemIntelRepo struct {
	mu    sync.Mutex
	seq   int
	wl    map[string]IntelWhitelist
	zones map[string]IntelZoneState
}

// NewMemIntelRepo 内存仓储。
func NewMemIntelRepo() *MemIntelRepo {
	return &MemIntelRepo{wl: map[string]IntelWhitelist{}, zones: map[string]IntelZoneState{}}
}

func (r *MemIntelRepo) ListWhitelist(_ context.Context) ([]IntelWhitelist, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]IntelWhitelist, 0, len(r.wl))
	for _, w := range r.wl {
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Value < out[j].Value })
	return out, nil
}

func (r *MemIntelRepo) CreateWhitelist(_ context.Context, w IntelWhitelist) (IntelWhitelist, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.wl {
		if e.Kind == w.Kind && strings.EqualFold(e.Value, w.Value) {
			return IntelWhitelist{}, ErrWhitelistDup
		}
	}
	r.seq++
	w.ID = fmt.Sprintf("wl-%d", r.seq)
	if w.CreatedAt.IsZero() {
		w.CreatedAt = time.Now().UTC()
	}
	r.wl[w.ID] = w
	return w, nil
}

func (r *MemIntelRepo) DeleteWhitelist(_ context.Context, id string) (IntelWhitelist, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.wl[id]
	if !ok {
		return IntelWhitelist{}, ErrWhitelistNotFound
	}
	delete(r.wl, id)
	return w, nil
}

func (r *MemIntelRepo) ListZones(_ context.Context) ([]IntelZoneState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]IntelZoneState, 0, len(r.zones))
	for _, z := range r.zones {
		out = append(out, z)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Zone < out[j].Zone })
	return out, nil
}

func (r *MemIntelRepo) UpsertZone(_ context.Context, z IntelZoneState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if z.UpdatedAt.IsZero() {
		z.UpdatedAt = time.Now().UTC()
	}
	r.zones[z.Zone] = z
	return nil
}

// ── PG 实现（迁移 0044）──

// PgIntelRepo PG 实现。
type PgIntelRepo struct{ pool *pgxpool.Pool }

// NewPgIntelRepo PG 仓储。
func NewPgIntelRepo(pool *pgxpool.Pool) *PgIntelRepo { return &PgIntelRepo{pool: pool} }

func (r *PgIntelRepo) ListWhitelist(ctx context.Context) ([]IntelWhitelist, error) {
	rows, err := r.pool.Query(ctx, `SELECT id::text, kind, value, reason, coalesce(category,''),
		coalesce(source,'manual'), expires_at, enabled, coalesce(created_by,''), created_at
		FROM intel_whitelist ORDER BY value`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IntelWhitelist{}
	for rows.Next() {
		var w IntelWhitelist
		if err := rows.Scan(&w.ID, &w.Kind, &w.Value, &w.Reason, &w.Category,
			&w.Source, &w.ExpiresAt, &w.Enabled, &w.CreatedBy, &w.CreatedAt); err == nil {
			out = append(out, w)
		}
	}
	return out, rows.Err()
}

func (r *PgIntelRepo) CreateWhitelist(ctx context.Context, w IntelWhitelist) (IntelWhitelist, error) {
	var id string
	err := r.pool.QueryRow(ctx, `INSERT INTO intel_whitelist(kind,value,reason,category,source,expires_at,enabled,created_by)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (kind,value) DO NOTHING
		RETURNING id::text`,
		w.Kind, w.Value, w.Reason, nullStr(w.Category), w.Source, w.ExpiresAt, w.Enabled, nullStr(w.CreatedBy)).Scan(&id)
	if err == pgx.ErrNoRows {
		return IntelWhitelist{}, ErrWhitelistDup
	}
	if err != nil {
		return IntelWhitelist{}, err
	}
	w.ID = id
	if w.CreatedAt.IsZero() {
		w.CreatedAt = time.Now().UTC()
	}
	return w, nil
}

func (r *PgIntelRepo) DeleteWhitelist(ctx context.Context, id string) (IntelWhitelist, error) {
	var w IntelWhitelist
	err := r.pool.QueryRow(ctx, `DELETE FROM intel_whitelist WHERE id=$1
		RETURNING id::text, kind, value, reason, coalesce(category,''), coalesce(source,'manual'),
		expires_at, enabled, coalesce(created_by,''), created_at`, id).
		Scan(&w.ID, &w.Kind, &w.Value, &w.Reason, &w.Category, &w.Source,
			&w.ExpiresAt, &w.Enabled, &w.CreatedBy, &w.CreatedAt)
	if err == pgx.ErrNoRows {
		return IntelWhitelist{}, ErrWhitelistNotFound
	}
	return w, err
}

func (r *PgIntelRepo) ListZones(ctx context.Context) ([]IntelZoneState, error) {
	rows, err := r.pool.Query(ctx, `SELECT zone_name, entries, soa_serial, loaded_serial,
		coalesce(sha256,''), coalesce(file_path,''), status, updated_at FROM intel_zone ORDER BY zone_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IntelZoneState{}
	for rows.Next() {
		var z IntelZoneState
		var serial, loaded int64
		if err := rows.Scan(&z.Zone, &z.Entries, &serial, &loaded, &z.SHA, &z.Path, &z.Status, &z.UpdatedAt); err == nil {
			z.Serial, z.LoadedSerial = uint32(serial), uint32(loaded)
			out = append(out, z)
		}
	}
	return out, rows.Err()
}

func (r *PgIntelRepo) UpsertZone(ctx context.Context, z IntelZoneState) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO intel_zone(zone_name,entries,soa_serial,loaded_serial,sha256,file_path,status,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,now())
		ON CONFLICT (zone_name) DO UPDATE SET entries=EXCLUDED.entries, soa_serial=EXCLUDED.soa_serial,
		loaded_serial=EXCLUDED.loaded_serial, sha256=EXCLUDED.sha256, file_path=EXCLUDED.file_path,
		status=EXCLUDED.status, updated_at=now()`,
		z.Zone, z.Entries, int64(z.Serial), int64(z.LoadedSerial), z.SHA, z.Path, z.Status)
	return err
}
