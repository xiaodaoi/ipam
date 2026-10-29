package backup

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MemRepo 内存实现（测试/纯内存 PoC）。
type MemRepo struct {
	mu   sync.RWMutex
	m    map[string]Record
	list []string
}

func NewMemRepo() *MemRepo { return &MemRepo{m: map[string]Record{}} }

func (r *MemRepo) Create(_ context.Context, rec *Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[rec.ID] = *rec
	r.list = append([]string{rec.ID}, r.list...)
	return nil
}

func (r *MemRepo) Update(_ context.Context, rec *Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.m[rec.ID]; !ok {
		return errors.New("not found")
	}
	r.m[rec.ID] = *rec
	return nil
}

func (r *MemRepo) Get(_ context.Context, id string) (Record, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.m[id]
	return rec, ok, nil
}

func (r *MemRepo) List(_ context.Context, limit int) ([]Record, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Record, 0, len(r.list))
	for _, id := range r.list {
		if len(out) >= limit {
			break
		}
		if rec, ok := r.m[id]; ok {
			out = append(out, rec)
		}
	}
	return out, nil
}

func (r *MemRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.m, id)
	for i, v := range r.list {
		if v == id {
			r.list = append(r.list[:i], r.list[i+1:]...)
			break
		}
	}
	return nil
}

// PgRepo PostgreSQL 实现。
type PgRepo struct{ pool *pgxpool.Pool }

func NewPgRepo(pool *pgxpool.Pool) *PgRepo { return &PgRepo{pool: pool} }

const recCols = `id::text, kind, status, note, dir, size_bytes, checksum, months, error, started_at, finished_at`

func scanRecord(row pgx.Row) (Record, error) {
	var r Record
	var finished *time.Time
	err := row.Scan(&r.ID, &r.Kind, &r.Status, &r.Note, &r.Dir, &r.SizeBytes, &r.Checksum, &r.Months, &r.Error, &r.StartedAt, &finished)
	if r.Months == nil {
		r.Months = []string{}
	}
	r.FinishedAt = finished
	return r, err
}

func (r *PgRepo) Create(ctx context.Context, rec *Record) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO backup_record(id, kind, status, note, dir, started_at)
		 VALUES($1,$2,$3,$4,$5,$6)`,
		rec.ID, rec.Kind, rec.Status, rec.Note, rec.Dir, rec.StartedAt)
	return err
}

func (r *PgRepo) Update(ctx context.Context, rec *Record) error {
	months := rec.Months
	if months == nil {
		months = []string{}
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE backup_record SET status=$2, size_bytes=$3, checksum=$4, months=$5, error=$6, finished_at=$7
		 WHERE id=$1`,
		rec.ID, rec.Status, rec.SizeBytes, rec.Checksum, months, rec.Error, rec.FinishedAt)
	return err
}

func (r *PgRepo) Get(ctx context.Context, id string) (Record, bool, error) {
	rec, err := scanRecord(r.pool.QueryRow(ctx, `SELECT `+recCols+` FROM backup_record WHERE id::text=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, false, nil
	}
	return rec, err == nil, err
}

func (r *PgRepo) List(ctx context.Context, limit int) ([]Record, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+recCols+` FROM backup_record ORDER BY started_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (r *PgRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM backup_record WHERE id::text=$1`, id)
	return err
}

// MemSettings 内存策略。
type MemSettings struct{ s Settings }

func NewMemSettings() *MemSettings {
	return &MemSettings{s: Settings{Enabled: true, ScheduleHour: 3, RetentionCount: 7}}
}

func (m *MemSettings) Get(context.Context) (Settings, error) { return m.s, nil }
func (m *MemSettings) Save(_ context.Context, s Settings) (Settings, error) {
	s.UpdatedAt = time.Now().UTC()
	m.s = s
	return s, nil
}

// PgSettings PostgreSQL 单行表。
type PgSettings struct{ pool *pgxpool.Pool }

func NewPgSettings(pool *pgxpool.Pool) *PgSettings { return &PgSettings{pool: pool} }

func (s *PgSettings) Get(ctx context.Context) (Settings, error) {
	var out Settings
	err := s.pool.QueryRow(ctx,
		`SELECT enabled, schedule_hour, retention_count, updated_at FROM backup_settings WHERE id=1`).
		Scan(&out.Enabled, &out.ScheduleHour, &out.RetentionCount, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Settings{Enabled: true, ScheduleHour: 3, RetentionCount: 7}, nil
	}
	return out, err
}

func (s *PgSettings) Save(ctx context.Context, in Settings) (Settings, error) {
	var out Settings
	err := s.pool.QueryRow(ctx,
		`INSERT INTO backup_settings(id, enabled, schedule_hour, retention_count, updated_at)
		 VALUES(1,$1,$2,$3,now())
		 ON CONFLICT (id) DO UPDATE SET enabled=EXCLUDED.enabled, schedule_hour=EXCLUDED.schedule_hour,
		   retention_count=EXCLUDED.retention_count, updated_at=now()
		 RETURNING enabled, schedule_hour, retention_count, updated_at`,
		in.Enabled, in.ScheduleHour, in.RetentionCount).
		Scan(&out.Enabled, &out.ScheduleHour, &out.RetentionCount, &out.UpdatedAt)
	return out, err
}
