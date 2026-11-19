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
	"github.com/jackc/pgx/v5/pgconn"
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

	// 情报源（M6-039）
	ListSources(ctx context.Context) ([]IntelSource, error)
	GetSource(ctx context.Context, id string) (IntelSource, bool, error)
	CreateSource(ctx context.Context, src IntelSource) (IntelSource, error)
	UpdateSource(ctx context.Context, src IntelSource) error
	UpdateSourceSync(ctx context.Context, id string, entries int, at time.Time, status, errMsg string) error
	DeleteSource(ctx context.Context, id string) error
	// UpsertPresetSource 仅当 key 不存在时插入（不覆盖用户维护的 URL/开关）。
	UpsertPresetSource(ctx context.Context, src IntelSource) error
}

// ── 内存实现（无 DB 的开发/单测路径）──

// MemIntelRepo 内存实现。
type MemIntelRepo struct {
	mu      sync.Mutex
	seq     int
	srcSeq  int
	wl      map[string]IntelWhitelist
	zones   map[string]IntelZoneState
	sources map[string]IntelSource
}

// NewMemIntelRepo 内存仓储。
func NewMemIntelRepo() *MemIntelRepo {
	return &MemIntelRepo{
		wl:      map[string]IntelWhitelist{},
		zones:   map[string]IntelZoneState{},
		sources: map[string]IntelSource{},
	}
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

// ── 内存实现：情报源 ──

func (r *MemIntelRepo) ListSources(_ context.Context) ([]IntelSource, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]IntelSource, 0, len(r.sources))
	for _, s := range r.sources {
		out = append(out, hydrateSource(s))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Builtin != out[j].Builtin {
			return out[i].Builtin
		}
		return out[i].Key < out[j].Key
	})
	return out, nil
}

func (r *MemIntelRepo) GetSource(_ context.Context, id string) (IntelSource, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	src, ok := r.sources[id]
	if !ok {
		return IntelSource{}, false, nil
	}
	return hydrateSource(src), true, nil
}

func (r *MemIntelRepo) CreateSource(_ context.Context, src IntelSource) (IntelSource, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.sources {
		if strings.EqualFold(e.Key, src.Key) {
			return IntelSource{}, ErrSourceDupKey
		}
	}
	r.srcSeq++
	if src.ID == "" {
		src.ID = fmt.Sprintf("src-%d", r.srcSeq)
	}
	if src.Key == "" {
		src.Key = "custom:" + src.ID
	}
	now := time.Now().UTC()
	src.CreatedAt, src.UpdatedAt = now, now
	r.sources[src.ID] = src
	return src, nil
}

func (r *MemIntelRepo) UpdateSource(_ context.Context, src IntelSource) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sources[src.ID]; !ok {
		return ErrSourceNotFound
	}
	r.sources[src.ID] = src
	return nil
}

func (r *MemIntelRepo) UpdateSourceSync(_ context.Context, id string, entries int, at time.Time, status, errMsg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	src, ok := r.sources[id]
	if !ok {
		return ErrSourceNotFound
	}
	src.Entries = entries
	ts := at
	src.LastSyncAt = &ts
	src.LastStatus = status
	src.LastError = errMsg
	r.sources[id] = src
	return nil
}

func (r *MemIntelRepo) DeleteSource(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sources[id]; !ok {
		return ErrSourceNotFound
	}
	delete(r.sources, id)
	return nil
}

// hydrateSource 派生字段（与 PG scanSource 保持一致）。
func hydrateSource(s IntelSource) IntelSource {
	s.Builtin = s.Kind == "preset"
	s.HasSecret = s.SecretFile != ""
	return s
}

func (r *MemIntelRepo) UpsertPresetSource(_ context.Context, src IntelSource) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.sources {
		if e.Key == src.Key {
			return nil
		}
	}
	r.srcSeq++
	src.ID = fmt.Sprintf("src-%d", r.srcSeq)
	r.sources[src.ID] = src
	return nil
}

// ── PG 实现：情报源（迁移 0045）──

// isUniqueViolation PG 唯一键冲突（23505）。
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

const sourceColumns = `id::text, key, name, kind, layer, url, format, auth_header,
	secret_file, license, license_url, commercial_ok, requires_ack, acknowledged_at,
	update_interval, enabled, entries, last_sync_at, last_status, last_error, created_at, updated_at`

func scanSource(sc interface{ Scan(...any) error }) (IntelSource, error) {
	var s IntelSource
	var entries int64
	err := sc.Scan(&s.ID, &s.Key, &s.Name, &s.Kind, &s.Layer, &s.URL, &s.Format, &s.AuthHeader,
		&s.SecretFile, &s.License, &s.LicenseURL, &s.CommercialOK, &s.RequiresAck, &s.AckedAt,
		&s.UpdateInterval, &s.Enabled, &entries, &s.LastSyncAt, &s.LastStatus, &s.LastError,
		&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return IntelSource{}, err
	}
	s.Entries = int(entries)
	s.HasSecret = s.SecretFile != ""
	s.Builtin = s.Kind == "preset"
	return s, nil
}

func (r *PgIntelRepo) ListSources(ctx context.Context) ([]IntelSource, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+sourceColumns+` FROM intel_source
		ORDER BY (kind='preset') DESC, key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IntelSource{}
	for rows.Next() {
		src, serr := scanSource(rows)
		if serr != nil {
			return nil, serr
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

func (r *PgIntelRepo) GetSource(ctx context.Context, id string) (IntelSource, bool, error) {
	src, err := scanSource(r.pool.QueryRow(ctx, `SELECT `+sourceColumns+` FROM intel_source WHERE id=$1`, id))
	if err == pgx.ErrNoRows {
		return IntelSource{}, false, nil
	}
	if err != nil {
		return IntelSource{}, false, err
	}
	return src, true, nil
}

func (r *PgIntelRepo) CreateSource(ctx context.Context, src IntelSource) (IntelSource, error) {
	row := r.pool.QueryRow(ctx, `INSERT INTO intel_source(id,key,name,kind,layer,url,format,auth_header,
		secret_file,license,license_url,commercial_ok,requires_ack,acknowledged_at,update_interval,
		enabled,entries,last_status)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
		RETURNING `+sourceColumns,
		src.ID, src.Key, src.Name, src.Kind, src.Layer, src.URL, src.Format, src.AuthHeader,
		src.SecretFile, src.License, src.LicenseURL, src.CommercialOK, src.RequiresAck, src.AckedAt,
		src.UpdateInterval, src.Enabled, int64(src.Entries), "never")
	out, err := scanSource(row)
	if err != nil {
		if isUniqueViolation(err) {
			return IntelSource{}, ErrSourceDupKey
		}
		return IntelSource{}, err
	}
	return out, nil
}

func (r *PgIntelRepo) UpdateSource(ctx context.Context, src IntelSource) error {
	_, err := r.pool.Exec(ctx, `UPDATE intel_source SET name=$2, layer=$3, url=$4, format=$5,
		auth_header=$6, secret_file=$7, update_interval=$8, enabled=$9, acknowledged_at=$10,
		updated_at=now() WHERE id=$1`,
		src.ID, src.Name, src.Layer, src.URL, src.Format, src.AuthHeader, src.SecretFile,
		src.UpdateInterval, src.Enabled, src.AckedAt)
	if err != nil && isUniqueViolation(err) {
		return ErrSourceDupKey
	}
	return err
}

func (r *PgIntelRepo) UpdateSourceSync(ctx context.Context, id string, entries int, at time.Time, status, errMsg string) error {
	_, err := r.pool.Exec(ctx, `UPDATE intel_source SET entries=$2, last_sync_at=$3,
		last_status=$4, last_error=$5, updated_at=now() WHERE id=$1`,
		id, int64(entries), at, status, errMsg)
	return err
}

func (r *PgIntelRepo) DeleteSource(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM intel_source WHERE id=$1 AND kind='custom'`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrSourceBuiltin
	}
	return nil
}

func (r *PgIntelRepo) UpsertPresetSource(ctx context.Context, src IntelSource) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO intel_source(key,name,kind,layer,url,format,auth_header,
		license,license_url,commercial_ok,requires_ack,update_interval,enabled,entries,last_status)
		VALUES($1,$2,'preset',$3,$4,$5,$6,$7,$8,$9,$10,$11,false,0,'never')
		ON CONFLICT (key) DO NOTHING`,
		src.Key, src.Name, src.Layer, src.URL, src.Format, src.AuthHeader,
		src.License, src.LicenseURL, src.CommercialOK, src.RequiresAck, src.UpdateInterval)
	return err
}
