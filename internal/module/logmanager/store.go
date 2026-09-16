package logmanager

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrArchiveNotFound 归档不存在（下载/删除时返回 404）。
var ErrArchiveNotFound = errors.New("log archive not found")

// Settings 日志存储策略（单行表 log_settings）。
type Settings struct {
	RetentionDays     int  // ClickHouse TTL 天数
	ArchiveKeepMonths int  // 归档文件保留月数
	AutoExport        bool // 每月 1 日自动导出上月
}

// DefaultSettings 默认策略（迁移 0026 同值）。
func DefaultSettings() Settings {
	return Settings{RetentionDays: 180, ArchiveKeepMonths: 12, AutoExport: true}
}

// SettingsRepo 存储策略读写抽象。
type SettingsRepo interface {
	Get(ctx context.Context) (Settings, error)
	Save(ctx context.Context, v Settings) error
}

// MemSettingsRepo 内存实现（PoC/单测）。
type MemSettingsRepo struct {
	mu sync.Mutex
	v  Settings
}

func NewMemSettingsRepo() *MemSettingsRepo {
	return &MemSettingsRepo{v: DefaultSettings()}
}

func (s *MemSettingsRepo) Get(_ context.Context) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.v, nil
}

func (s *MemSettingsRepo) Save(_ context.Context, v Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.v = v
	return nil
}

// PgSettingsRepo PG 实现（log_settings 单行表，迁移 0026）。
type PgSettingsRepo struct {
	pool *pgxpool.Pool
}

func NewPgSettingsRepo(pool *pgxpool.Pool) *PgSettingsRepo { return &PgSettingsRepo{pool: pool} }

func (s *PgSettingsRepo) Get(ctx context.Context) (Settings, error) {
	var v Settings
	err := s.pool.QueryRow(ctx,
		`SELECT coalesce(retention_days, 180), coalesce(archive_keep_months, 12), coalesce(auto_export, true)
		   FROM log_settings WHERE id`).
		Scan(&v.RetentionDays, &v.ArchiveKeepMonths, &v.AutoExport)
	if err != nil {
		return DefaultSettings(), err
	}
	return v, nil
}

func (s *PgSettingsRepo) Save(ctx context.Context, v Settings) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO log_settings (id, retention_days, archive_keep_months, auto_export) VALUES (true, $1, $2, $3)
		 ON CONFLICT (id) DO UPDATE SET retention_days = EXCLUDED.retention_days,
		   archive_keep_months = EXCLUDED.archive_keep_months, auto_export = EXCLUDED.auto_export, updated_at = now()`,
		v.RetentionDays, v.ArchiveKeepMonths, v.AutoExport)
	return err
}

// Archive 按月归档元数据（log_archive 表）。
type Archive struct {
	Month      time.Time // 当月 1 日（UTC）
	RowCount   int64
	FileSize   int64
	FilePath   string // 归档文件名（相对导出目录）
	FileHash   string // SHA256
	ExportedBy string // system | manual
	CreatedAt  time.Time
}

// ArchiveRepo 归档元数据读写抽象。
type ArchiveRepo interface {
	List(ctx context.Context) ([]Archive, error)
	Get(ctx context.Context, month time.Time) (Archive, error)
	Upsert(ctx context.Context, a Archive) error
	Delete(ctx context.Context, month time.Time) error
}

// MemArchiveRepo 内存实现（PoC/单测）。
type MemArchiveRepo struct {
	mu sync.Mutex
	m  map[string]Archive
}

func NewMemArchiveRepo() *MemArchiveRepo { return &MemArchiveRepo{m: map[string]Archive{}} }

func monthKey(t time.Time) string { return t.UTC().Format("2006-01") }

func (r *MemArchiveRepo) List(_ context.Context) ([]Archive, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Archive, 0, len(r.m))
	for _, a := range r.m {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Month.After(out[j].Month) })
	return out, nil
}

func (r *MemArchiveRepo) Get(_ context.Context, month time.Time) (Archive, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.m[monthKey(month)]
	if !ok {
		return Archive{}, ErrArchiveNotFound
	}
	return a, nil
}

func (r *MemArchiveRepo) Upsert(_ context.Context, a Archive) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[monthKey(a.Month)] = a
	return nil
}

func (r *MemArchiveRepo) Delete(_ context.Context, month time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.m, monthKey(month))
	return nil
}

// PgArchiveRepo PG 实现（log_archive 表，迁移 0026）。
type PgArchiveRepo struct {
	pool *pgxpool.Pool
}

func NewPgArchiveRepo(pool *pgxpool.Pool) *PgArchiveRepo { return &PgArchiveRepo{pool: pool} }

func (r *PgArchiveRepo) List(ctx context.Context) ([]Archive, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT month, row_count, file_size, file_path, file_hash, exported_by, created_at
		   FROM log_archive ORDER BY month DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Archive, 0)
	for rows.Next() {
		var a Archive
		if err := rows.Scan(&a.Month, &a.RowCount, &a.FileSize, &a.FilePath, &a.FileHash, &a.ExportedBy, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *PgArchiveRepo) Get(ctx context.Context, month time.Time) (Archive, error) {
	var a Archive
	err := r.pool.QueryRow(ctx,
		`SELECT month, row_count, file_size, file_path, file_hash, exported_by, created_at
		   FROM log_archive WHERE month = $1`, month.UTC()).
		Scan(&a.Month, &a.RowCount, &a.FileSize, &a.FilePath, &a.FileHash, &a.ExportedBy, &a.CreatedAt)
	if err != nil {
		return Archive{}, err
	}
	return a, nil
}

func (r *PgArchiveRepo) Upsert(ctx context.Context, a Archive) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO log_archive (month, row_count, file_size, file_path, file_hash, exported_by, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, now())
		 ON CONFLICT (month) DO UPDATE SET row_count = EXCLUDED.row_count, file_size = EXCLUDED.file_size,
		   file_path = EXCLUDED.file_path, file_hash = EXCLUDED.file_hash, exported_by = EXCLUDED.exported_by`,
		a.Month.UTC(), a.RowCount, a.FileSize, a.FilePath, a.FileHash, a.ExportedBy)
	return err
}

func (r *PgArchiveRepo) Delete(ctx context.Context, month time.Time) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM log_archive WHERE month = $1`, month.UTC())
	return err
}
