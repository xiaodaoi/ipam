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

// IntelSourceRun 情报源一次同步/回滚执行记录（M6-039 P2）。
type IntelSourceRun struct {
	ID         int64
	SourceID   string
	StartedAt  time.Time
	DurationMs int
	Status     string // ok | failed | rolled_back
	Trigger    string // manual | schedule | enable | rollback
	Entries    int
	Added      int
	Removed    int
	Excluded   int
	Revision   int
	Error      string
}

// IntelSourceRevision 情报源内容版本（成功同步的差异快照）。
type IntelSourceRevision struct {
	ID        int64
	SourceID  string
	Revision  int
	Entries   int
	Path      string
	SHA       string
	CreatedAt time.Time
	Active    bool
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
	UpdateSourceStats(ctx context.Context, id string, entries, added, removed, excluded, durationMs int, at time.Time, status, errMsg string) error
	DeleteSource(ctx context.Context, id string) error
	// UpsertPresetSource 仅当 key 不存在时插入（不覆盖用户维护的 URL/开关）。
	UpsertPresetSource(ctx context.Context, src IntelSource) error

	// 版本与执行记录（M6-039 P2）
	CreateSourceRevision(ctx context.Context, rev IntelSourceRevision) (IntelSourceRevision, error)
	ListSourceRevisions(ctx context.Context, sourceID string, limit int) ([]IntelSourceRevision, error)
	GetSourceRevision(ctx context.Context, sourceID string, revision int) (IntelSourceRevision, bool, error)
	// PruneSourceRevisions 只保留最近 keep 个版本，返回被删除版本的文件路径。
	PruneSourceRevisions(ctx context.Context, sourceID string, keep int) ([]string, error)
	RecordSourceRun(ctx context.Context, run IntelSourceRun) (IntelSourceRun, error)
	ListSourceRuns(ctx context.Context, sourceID string, limit int) ([]IntelSourceRun, error)
	PruneSourceRuns(ctx context.Context, sourceID string, keep int) error
}

// ── 内存实现（无 DB 的开发/单测路径）──

// MemIntelRepo 内存实现。
type MemIntelRepo struct {
	mu      sync.Mutex
	seq     int
	srcSeq  int
	runSeq  int64
	revSeq  int64
	wl      map[string]IntelWhitelist
	zones   map[string]IntelZoneState
	sources map[string]IntelSource
	runs    map[string][]IntelSourceRun
	revs    map[string][]IntelSourceRevision
}

// NewMemIntelRepo 内存仓储。
func NewMemIntelRepo() *MemIntelRepo {
	return &MemIntelRepo{
		wl:      map[string]IntelWhitelist{},
		zones:   map[string]IntelZoneState{},
		sources: map[string]IntelSource{},
		runs:    map[string][]IntelSourceRun{},
		revs:    map[string][]IntelSourceRevision{},
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

func (r *MemIntelRepo) UpdateSourceStats(_ context.Context, id string, entries, added, removed, excluded, durationMs int, at time.Time, status, errMsg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	src, ok := r.sources[id]
	if !ok {
		return ErrSourceNotFound
	}
	src.Entries = entries
	src.LastAdded = added
	src.LastRemoved = removed
	src.LastExcluded = excluded
	src.LastDurationMs = durationMs
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

// ── 内存实现：版本与执行记录（M6-039 P2）──

func (r *MemIntelRepo) CreateSourceRevision(_ context.Context, rev IntelSourceRevision) (IntelSourceRevision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.revSeq++
	rev.ID = r.revSeq
	if rev.CreatedAt.IsZero() {
		rev.CreatedAt = time.Now().UTC()
	}
	list := append(r.revs[rev.SourceID], rev)
	sort.Slice(list, func(i, j int) bool { return list[i].Revision > list[j].Revision })
	r.revs[rev.SourceID] = list
	return rev, nil
}

func (r *MemIntelRepo) ListSourceRevisions(_ context.Context, sourceID string, limit int) ([]IntelSourceRevision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := append([]IntelSourceRevision(nil), r.revs[sourceID]...)
	sort.Slice(list, func(i, j int) bool { return list[i].Revision > list[j].Revision })
	if limit > 0 && len(list) > limit {
		list = list[:limit]
	}
	return list, nil
}

func (r *MemIntelRepo) GetSourceRevision(_ context.Context, sourceID string, revision int) (IntelSourceRevision, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rev := range r.revs[sourceID] {
		if rev.Revision == revision {
			return rev, true, nil
		}
	}
	return IntelSourceRevision{}, false, nil
}

func (r *MemIntelRepo) PruneSourceRevisions(_ context.Context, sourceID string, keep int) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := append([]IntelSourceRevision(nil), r.revs[sourceID]...)
	sort.Slice(list, func(i, j int) bool { return list[i].Revision > list[j].Revision })
	if keep < 0 {
		keep = 0
	}
	if len(list) <= keep {
		return nil, nil
	}
	removed := list[keep:]
	paths := make([]string, 0, len(removed))
	for _, rev := range removed {
		paths = append(paths, rev.Path)
	}
	r.revs[sourceID] = list[:keep]
	return paths, nil
}

func (r *MemIntelRepo) RecordSourceRun(_ context.Context, run IntelSourceRun) (IntelSourceRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runSeq++
	run.ID = r.runSeq
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	}
	list := append([]IntelSourceRun{run}, r.runs[run.SourceID]...)
	r.runs[run.SourceID] = list
	return run, nil
}

func (r *MemIntelRepo) ListSourceRuns(_ context.Context, sourceID string, limit int) ([]IntelSourceRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := append([]IntelSourceRun(nil), r.runs[sourceID]...)
	if limit > 0 && len(list) > limit {
		list = list[:limit]
	}
	return list, nil
}

func (r *MemIntelRepo) PruneSourceRuns(_ context.Context, sourceID string, keep int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := r.runs[sourceID]
	if keep < 0 {
		keep = 0
	}
	if len(list) > keep {
		r.runs[sourceID] = list[:keep]
	}
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
	update_interval, max_share_pct, enabled, entries, last_sync_at, last_status, last_error,
	last_duration_ms, last_added, last_removed, last_excluded, created_at, updated_at`

func scanSource(sc interface{ Scan(...any) error }) (IntelSource, error) {
	var s IntelSource
	var entries int64
	err := sc.Scan(&s.ID, &s.Key, &s.Name, &s.Kind, &s.Layer, &s.URL, &s.Format, &s.AuthHeader,
		&s.SecretFile, &s.License, &s.LicenseURL, &s.CommercialOK, &s.RequiresAck, &s.AckedAt,
		&s.UpdateInterval, &s.MaxSharePct, &s.Enabled, &entries, &s.LastSyncAt, &s.LastStatus, &s.LastError,
		&s.LastDurationMs, &s.LastAdded, &s.LastRemoved, &s.LastExcluded, &s.CreatedAt, &s.UpdatedAt)
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
		max_share_pct,enabled,entries,last_status)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		RETURNING `+sourceColumns,
		src.ID, src.Key, src.Name, src.Kind, src.Layer, src.URL, src.Format, src.AuthHeader,
		src.SecretFile, src.License, src.LicenseURL, src.CommercialOK, src.RequiresAck, src.AckedAt,
		src.UpdateInterval, src.MaxSharePct, src.Enabled, int64(src.Entries), "never")
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
		auth_header=$6, secret_file=$7, update_interval=$8, max_share_pct=$9, enabled=$10,
		acknowledged_at=$11, updated_at=now() WHERE id=$1`,
		src.ID, src.Name, src.Layer, src.URL, src.Format, src.AuthHeader, src.SecretFile,
		src.UpdateInterval, src.MaxSharePct, src.Enabled, src.AckedAt)
	if err != nil && isUniqueViolation(err) {
		return ErrSourceDupKey
	}
	return err
}

func (r *PgIntelRepo) UpdateSourceStats(ctx context.Context, id string, entries, added, removed, excluded, durationMs int, at time.Time, status, errMsg string) error {
	_, err := r.pool.Exec(ctx, `UPDATE intel_source SET entries=$2, last_sync_at=$3,
		last_status=$4, last_error=$5, last_duration_ms=$6, last_added=$7, last_removed=$8,
		last_excluded=$9, updated_at=now() WHERE id=$1`,
		id, int64(entries), at, status, errMsg, durationMs, added, removed, excluded)
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

// ── PG 实现：版本与执行记录（M6-039 P2）──

func (r *PgIntelRepo) CreateSourceRevision(ctx context.Context, rev IntelSourceRevision) (IntelSourceRevision, error) {
	err := r.pool.QueryRow(ctx, `INSERT INTO intel_source_revision(source_id,revision,entries,path,sha256)
		VALUES($1,$2,$3,$4,$5) RETURNING id, created_at`,
		rev.SourceID, rev.Revision, rev.Entries, rev.Path, rev.SHA).Scan(&rev.ID, &rev.CreatedAt)
	return rev, err
}

func (r *PgIntelRepo) ListSourceRevisions(ctx context.Context, sourceID string, limit int) ([]IntelSourceRevision, error) {
	if limit <= 0 {
		limit = MaxSourceRevisions
	}
	rows, err := r.pool.Query(ctx, `SELECT id, source_id::text, revision, entries, path,
		coalesce(sha256,''), created_at FROM intel_source_revision
		WHERE source_id=$1 ORDER BY revision DESC LIMIT $2`, sourceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IntelSourceRevision{}
	for rows.Next() {
		var rev IntelSourceRevision
		if err := rows.Scan(&rev.ID, &rev.SourceID, &rev.Revision, &rev.Entries, &rev.Path,
			&rev.SHA, &rev.CreatedAt); err == nil {
			out = append(out, rev)
		}
	}
	return out, rows.Err()
}

func (r *PgIntelRepo) GetSourceRevision(ctx context.Context, sourceID string, revision int) (IntelSourceRevision, bool, error) {
	var rev IntelSourceRevision
	err := r.pool.QueryRow(ctx, `SELECT id, source_id::text, revision, entries, path,
		coalesce(sha256,''), created_at FROM intel_source_revision
		WHERE source_id=$1 AND revision=$2`, sourceID, revision).
		Scan(&rev.ID, &rev.SourceID, &rev.Revision, &rev.Entries, &rev.Path, &rev.SHA, &rev.CreatedAt)
	if err == pgx.ErrNoRows {
		return IntelSourceRevision{}, false, nil
	}
	if err != nil {
		return IntelSourceRevision{}, false, err
	}
	return rev, true, nil
}

func (r *PgIntelRepo) PruneSourceRevisions(ctx context.Context, sourceID string, keep int) ([]string, error) {
	if keep < 0 {
		keep = 0
	}
	rows, err := r.pool.Query(ctx, `WITH ranked AS (
		  SELECT id, path, row_number() OVER (ORDER BY revision DESC) AS rn
		  FROM intel_source_revision WHERE source_id=$1
		), del AS (
		  DELETE FROM intel_source_revision WHERE id IN (SELECT id FROM ranked WHERE rn > $2)
		  RETURNING path
		)
		SELECT path FROM del`, sourceID, keep)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	paths := []string{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err == nil {
			paths = append(paths, p)
		}
	}
	return paths, rows.Err()
}

func (r *PgIntelRepo) RecordSourceRun(ctx context.Context, run IntelSourceRun) (IntelSourceRun, error) {
	err := r.pool.QueryRow(ctx, `INSERT INTO intel_source_run(source_id,started_at,duration_ms,status,
		trigger,entries,added,removed,excluded,revision,error)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`,
		run.SourceID, run.StartedAt, run.DurationMs, run.Status, run.Trigger,
		run.Entries, run.Added, run.Removed, run.Excluded, run.Revision, run.Error).Scan(&run.ID)
	return run, err
}

func (r *PgIntelRepo) ListSourceRuns(ctx context.Context, sourceID string, limit int) ([]IntelSourceRun, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.pool.Query(ctx, `SELECT id, source_id::text, started_at, duration_ms, status,
		trigger, entries, added, removed, excluded, revision, coalesce(error,'')
		FROM intel_source_run WHERE source_id=$1 ORDER BY started_at DESC, id DESC LIMIT $2`,
		sourceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IntelSourceRun{}
	for rows.Next() {
		var run IntelSourceRun
		if err := rows.Scan(&run.ID, &run.SourceID, &run.StartedAt, &run.DurationMs, &run.Status,
			&run.Trigger, &run.Entries, &run.Added, &run.Removed, &run.Excluded, &run.Revision,
			&run.Error); err == nil {
			out = append(out, run)
		}
	}
	return out, rows.Err()
}

func (r *PgIntelRepo) PruneSourceRuns(ctx context.Context, sourceID string, keep int) error {
	if keep < 0 {
		keep = 0
	}
	_, err := r.pool.Exec(ctx, `DELETE FROM intel_source_run
		WHERE source_id=$1 AND id NOT IN (
		  SELECT id FROM intel_source_run WHERE source_id=$1
		  ORDER BY started_at DESC, id DESC LIMIT $2
		)`, sourceID, keep)
	return err
}
