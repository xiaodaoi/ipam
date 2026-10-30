package utilization

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MemRepo 内存实现（PoC/单测）。
type MemRepo struct {
	mu     sync.RWMutex
	points []Point
}

func NewMemRepo() *MemRepo { return &MemRepo{} }

func (m *MemRepo) Insert(_ context.Context, points []Point) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.points = append(m.points, points...)
	return nil
}

func (m *MemRepo) Latest(_ context.Context) ([]Point, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	latest := map[string]Point{}
	for _, p := range m.points {
		if old, ok := latest[p.SubnetID]; !ok || p.TS.After(old.TS) {
			latest[p.SubnetID] = p
		}
	}
	out := make([]Point, 0, len(latest))
	for _, p := range latest {
		out = append(out, p)
	}
	return out, nil
}

func (m *MemRepo) Series(_ context.Context, subnetID string, since time.Time) ([]Point, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Point, 0)
	for _, p := range m.points {
		if p.SubnetID == subnetID && !p.TS.Before(since) {
			out = append(out, p)
		}
	}
	return out, nil
}

func (m *MemRepo) Prune(_ context.Context, before time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.points[:0]
	for _, p := range m.points {
		if !p.TS.Before(before) {
			kept = append(kept, p)
		}
	}
	m.points = kept
	return nil
}

// PgRepo PostgreSQL 实现（迁移 0033_subnet_utilization.sql）。
type PgRepo struct{ pool *pgxpool.Pool }

func NewPgRepo(pool *pgxpool.Pool) *PgRepo { return &PgRepo{pool: pool} }

func (r *PgRepo) Insert(ctx context.Context, points []Point) error {
	if len(points) == 0 {
		return nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, p := range points {
		if _, err := tx.Exec(ctx,
			`INSERT INTO subnet_utilization(subnet_id, ts, used, capacity, pct) VALUES($1,$2,$3,$4,$5)`,
			p.SubnetID, p.TS, p.Used, p.Capacity, p.Pct); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

const pointCols = `u.subnet_id::text, s.name, s.cidr::text, u.ts, u.used, u.capacity, u.pct::float8`

func (r *PgRepo) Latest(ctx context.Context) ([]Point, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT ON (u.subnet_id) `+pointCols+`
		  FROM subnet_utilization u JOIN subnet s ON s.id = u.subnet_id
		 ORDER BY u.subnet_id, u.ts DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Point{}
	for rows.Next() {
		var p Point
		if err := rows.Scan(&p.SubnetID, &p.SubnetName, &p.CIDR, &p.TS, &p.Used, &p.Capacity, &p.Pct); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *PgRepo) Series(ctx context.Context, subnetID string, since time.Time) ([]Point, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+pointCols+`
		  FROM subnet_utilization u JOIN subnet s ON s.id = u.subnet_id
		 WHERE u.subnet_id=$1 AND u.ts >= $2
		 ORDER BY u.ts ASC`, subnetID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Point{}
	for rows.Next() {
		var p Point
		if err := rows.Scan(&p.SubnetID, &p.SubnetName, &p.CIDR, &p.TS, &p.Used, &p.Capacity, &p.Pct); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *PgRepo) Prune(ctx context.Context, before time.Time) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM subnet_utilization WHERE ts < $1`, before)
	return err
}
