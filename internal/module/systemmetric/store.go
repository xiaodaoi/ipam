package systemmetric

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ── 内存实现（PoC / 单测） ──────────────────────────────────────────────

type MemRepo struct {
	mu     sync.RWMutex
	points []Point
}

func NewMemRepo() *MemRepo { return &MemRepo{} }

func (m *MemRepo) Insert(_ context.Context, p Point) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.points {
		if m.points[i].Node == p.Node && m.points[i].TS.Equal(p.TS) {
			return nil // 与 PG 的 ON CONFLICT DO NOTHING 对齐
		}
	}
	m.points = append(m.points, p)
	return nil
}

func (m *MemRepo) Series(_ context.Context, node string, from, to time.Time, step time.Duration) ([]Point, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if step <= 0 {
		step = time.Minute
	}
	type bucket struct {
		sum Point
		n   int
	}
	buckets := map[int64]*bucket{}
	order := []int64{}
	for _, p := range m.points {
		if p.Node != node || p.TS.Before(from) || !p.TS.Before(to) {
			continue
		}
		key := p.TS.Truncate(step).Unix()
		b, ok := buckets[key]
		if !ok {
			b = &bucket{}
			buckets[key] = b
			order = append(order, key)
		}
		b.sum.TS = p.TS.Truncate(step)
		b.sum.CPUPct += p.CPUPct
		b.sum.MemPct += p.MemPct
		b.sum.Load1 += p.Load1
		b.sum.Load5 += p.Load5
		b.sum.Load15 += p.Load15
		b.sum.DiskRootPct += p.DiskRootPct
		b.sum.Goroutines += p.Goroutines
		b.sum.HeapBytes += p.HeapBytes
		b.sum.DBPoolInUse += p.DBPoolInUse
		if p.DiskMaxPct >= b.sum.DiskMaxPct {
			b.sum.DiskMaxPct = p.DiskMaxPct
			b.sum.DiskWorstMount = p.DiskWorstMount
		}
		if p.Cores > b.sum.Cores {
			b.sum.Cores = p.Cores
		}
		b.n++
	}
	sortInt64(order)
	out := make([]Point, 0, len(order))
	for _, k := range order {
		b := buckets[k]
		p := b.sum
		p.Node = node
		n := float64(b.n)
		p.CPUPct /= n
		p.MemPct /= n
		p.Load1 /= n
		p.Load5 /= n
		p.Load15 /= n
		p.DiskRootPct /= n
		p.Goroutines = int(float64(p.Goroutines) / n)
		p.HeapBytes = int64(float64(p.HeapBytes) / n)
		p.DBPoolInUse = int(float64(p.DBPoolInUse) / n)
		out = append(out, p)
	}
	return out, nil
}

func (m *MemRepo) Summary(_ context.Context, node string, from time.Time) (Summary, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sum := Summary{Node: node, From: from, DiskMount: "", LogLatestAgeMaxSec: -1}
	var cpuSum, memSum, diskSum float64
	for _, p := range m.points {
		if p.Node != node || p.TS.Before(from) {
			continue
		}
		if sum.Samples == 0 || p.TS.Before(sum.Since) {
			sum.Since = p.TS
		}
		sum.Samples++
		if p.CPUPct > sum.CPUMax {
			sum.CPUMax, sum.CPUMaxAt = p.CPUPct, p.TS
		}
		if p.MemPct > sum.MemMax {
			sum.MemMax, sum.MemMaxAt = p.MemPct, p.TS
		}
		if p.DiskMaxPct > sum.DiskMax {
			sum.DiskMax, sum.DiskMaxAt = p.DiskMaxPct, p.TS
			sum.DiskMount = p.DiskWorstMount
		}
		if p.Load1 > sum.Load1Max {
			sum.Load1Max, sum.Load1MaxAt = p.Load1, p.TS
		}
		if p.Goroutines > sum.GoroutinesMax {
			sum.GoroutinesMax = p.Goroutines
		}
		if p.HeapBytes > sum.HeapMaxBytes {
			sum.HeapMaxBytes = p.HeapBytes
		}
		if p.DBPoolInUse > sum.DBPoolInUseMax {
			sum.DBPoolInUseMax = p.DBPoolInUse
		}
		if p.LogLatestAgeSec > sum.LogLatestAgeMaxSec {
			sum.LogLatestAgeMaxSec = p.LogLatestAgeSec
		}
		cpuSum += p.CPUPct
		memSum += p.MemPct
		diskSum += p.DiskMaxPct
	}
	if sum.Samples > 0 {
		n := float64(sum.Samples)
		sum.CPUAvg, sum.MemAvg, sum.DiskAvg = cpuSum/n, memSum/n, diskSum/n
	}
	return sum, nil
}

func (m *MemRepo) Earliest(_ context.Context, node string) (time.Time, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var earliest time.Time
	found := false
	for _, p := range m.points {
		if p.Node != node {
			continue
		}
		if !found || p.TS.Before(earliest) {
			earliest, found = p.TS, true
		}
	}
	return earliest, found, nil
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

func sortInt64(v []int64) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j] < v[j-1]; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}

// ── PostgreSQL 实现（迁移 0043_system_metric） ───────────────────────────

type PgRepo struct{ pool *pgxpool.Pool }

func NewPgRepo(pool *pgxpool.Pool) *PgRepo { return &PgRepo{pool: pool} }

func (r *PgRepo) Insert(ctx context.Context, p Point) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO system_metric(node, ts, cpu_pct, cores, load1, load5, load15,
			mem_total_bytes, mem_used_bytes, mem_pct, uptime_sec,
			disk_root_pct, disk_max_pct, disk_worst_mount,
			goroutines, heap_bytes, db_pool_in_use, db_pool_max, log_latest_age_sec)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		ON CONFLICT (node, ts) DO NOTHING
		RETURNING id`,
		p.Node, p.TS, p.CPUPct, p.Cores, p.Load1, p.Load5, p.Load15,
		p.MemTotalBytes, p.MemUsedBytes, p.MemPct, p.UptimeSec,
		p.DiskRootPct, p.DiskMaxPct, nullIfEmpty(p.DiskWorstMount),
		p.Goroutines, p.HeapBytes, p.DBPoolInUse, p.DBPoolMax, p.LogLatestAgeSec,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx) // 重复样本：明细也不再写
	}
	if err != nil {
		return err
	}
	for _, d := range p.Disks {
		if _, err := tx.Exec(ctx, `
			INSERT INTO system_disk_metric(metric_id, mount, device, fstype, total_bytes, used_bytes, used_pct)
			VALUES($1,$2,$3,$4,$5,$6,$7)`,
			id, d.Mount, nullIfEmpty(d.Device), nullIfEmpty(d.FSType),
			d.TotalBytes, d.UsedBytes, d.UsedPct); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

const seriesSQL = `
SELECT date_bin($3::interval, ts, timestamptz '2000-01-01 00:00:00+00') AS bucket,
       coalesce(avg(cpu_pct),0)::float8,
       coalesce(max(cores),0),
       coalesce(avg(load1),0)::float8,
       coalesce(avg(load5),0)::float8,
       coalesce(avg(load15),0)::float8,
       coalesce(avg(mem_total_bytes),0)::bigint,
       coalesce(avg(mem_used_bytes),0)::bigint,
       coalesce(avg(mem_pct),0)::float8,
       coalesce(avg(uptime_sec),0)::float8,
       coalesce(avg(disk_root_pct),0)::float8,
       coalesce(max(disk_max_pct),0)::float8,
       coalesce((array_agg(disk_worst_mount ORDER BY disk_max_pct DESC NULLS LAST))[1], '')::text,
       coalesce(avg(goroutines),0)::int,
       coalesce(avg(heap_bytes),0)::bigint,
       coalesce(avg(db_pool_in_use),0)::int,
       coalesce(max(db_pool_max),0),
       coalesce(avg(log_latest_age_sec),-1)::bigint
  FROM system_metric
 WHERE node=$1 AND ts >= $2 AND ts < $4
 GROUP BY bucket
 ORDER BY bucket`

func (r *PgRepo) Series(ctx context.Context, node string, from, to time.Time, step time.Duration) ([]Point, error) {
	if step < time.Minute {
		step = time.Minute
	}
	// 用「N seconds」文本传 interval，避免把 duration 当纳秒整数解释。
	stride := fmt.Sprintf("%d seconds", int64(step.Seconds()))
	rows, err := r.pool.Query(ctx, seriesSQL, node, from, stride, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Point{}
	for rows.Next() {
		var p Point
		p.Node = node
		p.LogLatestAgeSec = -1
		if err := rows.Scan(&p.TS, &p.CPUPct, &p.Cores, &p.Load1, &p.Load5, &p.Load15,
			&p.MemTotalBytes, &p.MemUsedBytes, &p.MemPct, &p.UptimeSec,
			&p.DiskRootPct, &p.DiskMaxPct, &p.DiskWorstMount,
			&p.Goroutines, &p.HeapBytes, &p.DBPoolInUse, &p.DBPoolMax, &p.LogLatestAgeSec); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

const summarySQL = `
SELECT count(*),
       coalesce(min(ts), now()),
       coalesce(max(ts), now()),
       coalesce(max(cpu_pct),0)::float8, coalesce(avg(cpu_pct),0)::float8,
       coalesce(max(mem_pct),0)::float8, coalesce(avg(mem_pct),0)::float8,
       coalesce(max(disk_max_pct),0)::float8, coalesce(avg(disk_max_pct),0)::float8,
       coalesce(max(load1),0)::float8,
       coalesce(max(goroutines),0)::int,
       coalesce(max(heap_bytes),0)::bigint,
       coalesce(max(db_pool_in_use),0)::int,
       coalesce(max(log_latest_age_sec),-1)::bigint
  FROM system_metric
 WHERE node=$1 AND ts >= $2`

func (r *PgRepo) Summary(ctx context.Context, node string, from time.Time) (Summary, error) {
	sum := Summary{Node: node, From: from, LogLatestAgeMaxSec: -1}
	err := r.pool.QueryRow(ctx, summarySQL, node, from).Scan(
		&sum.Samples, &sum.Since, &sum.To,
		&sum.CPUMax, &sum.CPUAvg, &sum.MemMax, &sum.MemAvg,
		&sum.DiskMax, &sum.DiskAvg, &sum.Load1Max,
		&sum.GoroutinesMax, &sum.HeapMaxBytes, &sum.DBPoolInUseMax, &sum.LogLatestAgeMaxSec)
	if err != nil {
		return sum, err
	}
	if sum.Samples == 0 {
		return sum, nil
	}
	_ = r.pool.QueryRow(ctx, `
		SELECT ts, coalesce(disk_worst_mount,'') FROM system_metric
		 WHERE node=$1 AND ts >= $2 AND disk_max_pct IS NOT NULL
		 ORDER BY disk_max_pct DESC LIMIT 1`, node, from).Scan(&sum.DiskMaxAt, &sum.DiskMount)
	_ = r.pool.QueryRow(ctx, `
		SELECT ts FROM system_metric WHERE node=$1 AND ts >= $2 AND cpu_pct IS NOT NULL
		 ORDER BY cpu_pct DESC LIMIT 1`, node, from).Scan(&sum.CPUMaxAt)
	_ = r.pool.QueryRow(ctx, `
		SELECT ts FROM system_metric WHERE node=$1 AND ts >= $2 AND mem_pct IS NOT NULL
		 ORDER BY mem_pct DESC LIMIT 1`, node, from).Scan(&sum.MemMaxAt)
	_ = r.pool.QueryRow(ctx, `
		SELECT ts FROM system_metric WHERE node=$1 AND ts >= $2 AND load1 IS NOT NULL
		 ORDER BY load1 DESC LIMIT 1`, node, from).Scan(&sum.Load1MaxAt)
	return sum, nil
}

func (r *PgRepo) Earliest(ctx context.Context, node string) (time.Time, bool, error) {
	var ts time.Time
	err := r.pool.QueryRow(ctx, `SELECT min(ts) FROM system_metric WHERE node=$1`, node).Scan(&ts)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, err
	}
	if ts.IsZero() {
		return time.Time{}, false, nil
	}
	return ts, true, nil
}

func (r *PgRepo) Prune(ctx context.Context, before time.Time) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM system_metric WHERE ts < $1`, before)
	return err
}
