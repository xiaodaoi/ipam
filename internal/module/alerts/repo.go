package alerts

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	guuid "github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const alertColumns = `id, dedup_key, source, kind, severity, status, title, message,
  client_ip, client_mac, domain, component, intel_source, intel_source_name,
  count, detail, first_seen, last_seen, acked_at, acked_by, resolved_at, created_at`

// PgRepo PostgreSQL 实现（迁移 0047）。
type PgRepo struct{ pool *pgxpool.Pool }

// NewPgRepo 构造。
func NewPgRepo(pool *pgxpool.Pool) *PgRepo { return &PgRepo{pool: pool} }

func likePattern(q string) string {
	q = strings.ReplaceAll(q, `\`, `\\`)
	q = strings.ReplaceAll(q, `%`, `\%`)
	q = strings.ReplaceAll(q, `_`, `\_`)
	return "%" + q + "%"
}

// splitIntelSources 把逗号分隔的来源 key/name 拆开并对齐；
// 显示名缺失时回退 key（历史/异常数据可读）。返回去重后的显示名列表。
func splitIntelSources(keys, names string) []string {
	ks := strings.Split(keys, ",")
	ns := strings.Split(names, ",")
	out := make([]string, 0, len(ks))
	seen := make(map[string]struct{}, len(ks))
	for i := range ks {
		name := ""
		if i < len(ns) {
			name = strings.TrimSpace(ns[i])
		}
		if name == "" {
			name = strings.TrimSpace(ks[i])
		}
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func buildWhere(f Filter) (string, []any) {
	conds := []string{"last_seen >= $1", "last_seen <= $2"}
	args := []any{f.From, f.To}
	add := func(expr, v string) {
		if v == "" {
			return
		}
		args = append(args, v)
		conds = append(conds, fmt.Sprintf(expr, len(args)))
	}
	add("source = $%d", f.Source)
	add("kind = $%d", f.Kind)
	add("severity = $%d", f.Severity)
	add("status = $%d", f.Status)
	if f.IntelSource != "" {
		args = append(args, likePattern(f.IntelSource))
		n := len(args)
		conds = append(conds, fmt.Sprintf(
			`(intel_source ILIKE $%d ESCAPE '\' OR intel_source_name ILIKE $%d ESCAPE '\')`, n, n))
	}
	if f.Q != "" {
		args = append(args, likePattern(f.Q))
		n := len(args)
		conds = append(conds, fmt.Sprintf(
			`(title ILIKE $%d ESCAPE '\' OR message ILIKE $%d ESCAPE '\' OR domain ILIKE $%d ESCAPE '\' OR client_ip ILIKE $%d ESCAPE '\' OR client_mac ILIKE $%d ESCAPE '\' OR component ILIKE $%d ESCAPE '\' OR intel_source ILIKE $%d ESCAPE '\' OR intel_source_name ILIKE $%d ESCAPE '\')`,
			n, n, n, n, n, n, n, n))
	}
	return strings.Join(conds, " AND "), args
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAlert(sc rowScanner) (Alert, error) {
	var (
		a                   Alert
		count               int64
		ackedAt, resolvedAt *time.Time
	)
	err := sc.Scan(&a.ID, &a.DedupKey, &a.Source, &a.Kind, &a.Severity, &a.Status,
		&a.Title, &a.Message, &a.ClientIP, &a.ClientMAC, &a.Domain, &a.Component,
		&a.IntelSource, &a.IntelSourceName,
		&count, &a.Detail, &a.FirstSeen, &a.LastSeen, &ackedAt, &a.AckedBy,
		&resolvedAt, &a.CreatedAt)
	if err != nil {
		return Alert{}, err
	}
	a.Count = int(count)
	a.AckedAt = ackedAt
	a.ResolvedAt = resolvedAt
	return a, nil
}

// List 分页查询（未确认优先，last_seen 倒序）。
func (r *PgRepo) List(ctx context.Context, f Filter) ([]Alert, int, error) {
	where, args := buildWhere(f)
	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM alert WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	qargs := append(append([]any{}, args...), f.PageSize, (f.Page-1)*f.PageSize)
	q := "SELECT " + alertColumns + " FROM alert WHERE " + where +
		" ORDER BY CASE status WHEN 'open' THEN 0 WHEN 'acked' THEN 1 ELSE 2 END, last_seen DESC, id" +
		" LIMIT $" + strconv.Itoa(len(args)+1) + " OFFSET $" + strconv.Itoa(len(args)+2)
	rows, err := r.pool.Query(ctx, q, qargs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]Alert, 0, f.PageSize)
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, a)
	}
	return items, int(total), rows.Err()
}

// Stats 统计聚合。
func (r *PgRepo) Stats(ctx context.Context, f Filter) (Stats, error) {
	where, args := buildWhere(f)
	st := Stats{BySeverity: map[string]int{}, ByKind: map[string]int{},
		ByIntelSource: map[string]int{}}

	rows, err := r.pool.Query(ctx,
		"SELECT status, severity, kind, count(*) FROM alert WHERE "+where+" GROUP BY 1,2,3", args...)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var status, severity, kind string
		var n int64
		if err := rows.Scan(&status, &severity, &kind, &n); err != nil {
			rows.Close()
			return st, err
		}
		st.Total += int(n)
		switch status {
		case StatusOpen:
			st.Open += int(n)
		case StatusAcked:
			st.Acked += int(n)
		case StatusResolved:
			st.Resolved += int(n)
		}
		st.BySeverity[severity] += int(n)
		st.ByKind[kind] += int(n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return st, err
	}

	top := func(col string) ([]Aggregate, error) {
		q := "SELECT " + col + ", sum(count) AS n FROM alert WHERE " + where +
			" AND " + col + " <> '' GROUP BY " + col + " ORDER BY n DESC, " + col + " LIMIT 10"
		rs, err := r.pool.Query(ctx, q, args...)
		if err != nil {
			return nil, err
		}
		defer rs.Close()
		out := make([]Aggregate, 0, 10)
		for rs.Next() {
			var key string
			var n int64
			if err := rs.Scan(&key, &n); err != nil {
				return nil, err
			}
			out = append(out, Aggregate{Key: key, Count: int(n)})
		}
		return out, rs.Err()
	}
	if st.TopDomains, err = top("domain"); err != nil {
		return st, err
	}
	if st.TopClients, err = top("client_ip"); err != nil {
		return st, err
	}

	srs, err := r.pool.Query(ctx,
		"SELECT intel_source, intel_source_name, count(*) FROM alert WHERE "+where+
			" AND intel_source <> '' GROUP BY 1,2", args...)
	if err != nil {
		return st, err
	}
	for srs.Next() {
		var keys, names string
		var n int64
		if err := srs.Scan(&keys, &names, &n); err != nil {
			srs.Close()
			return st, err
		}
		for _, name := range splitIntelSources(keys, names) {
			st.ByIntelSource[name] += int(n)
		}
	}
	srs.Close()
	if err := srs.Err(); err != nil {
		return st, err
	}

	bucket := f.Bucket
	if bucket != "hour" {
		bucket = "day"
	}
	targs := append(append([]any{}, args...), bucket)
	trs, err := r.pool.Query(ctx,
		"SELECT date_trunc($"+strconv.Itoa(len(targs))+", last_seen) AS b, count(*) FROM alert WHERE "+
			where+" GROUP BY b ORDER BY b", targs...)
	if err != nil {
		return st, err
	}
	defer trs.Close()
	st.Trend = make([]TrendPoint, 0, 32)
	for trs.Next() {
		var p TrendPoint
		var n int64
		if err := trs.Scan(&p.TS, &n); err != nil {
			return st, err
		}
		p.TS = p.TS.UTC()
		p.Count = int(n)
		st.Trend = append(st.Trend, p)
	}
	return st, trs.Err()
}

// Unread 未确认总数 + 最近 N 条。
func (r *PgRepo) Unread(ctx context.Context, limit int) (Unread, error) {
	var out Unread
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM alert WHERE status='open'").Scan(&out.Count); err != nil {
		return out, err
	}
	rows, err := r.pool.Query(ctx,
		"SELECT "+alertColumns+" FROM alert WHERE status='open' ORDER BY last_seen DESC, id LIMIT $1", limit)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	out.Items = make([]Alert, 0, limit)
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, a)
	}
	return out, rows.Err()
}

// Ack 确认指定报警（仅 open → acked，幂等）。
func (r *PgRepo) Ack(ctx context.Context, ids []string, actor string, at time.Time) (int, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE alert SET status='acked', acked_at=$1, acked_by=$2, updated_at=now()
		 WHERE id = ANY($3) AND status='open'`, at, actor, ids)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// AckAll 确认全部未确认。
func (r *PgRepo) AckAll(ctx context.Context, actor string, at time.Time) (int, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE alert SET status='acked', acked_at=$1, acked_by=$2, updated_at=now()
		 WHERE status='open'`, at, actor)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// Upsert 按 dedup_key 合并：同小时窗口累加，重复扫描不回退计数。
func (r *PgRepo) Upsert(ctx context.Context, ev Event) (Alert, error) {
	q := `INSERT INTO alert(dedup_key, source, kind, severity, status, title, message,
		client_ip, client_mac, domain, component, intel_source, intel_source_name,
		count, detail, first_seen, last_seen)
		VALUES($1,$2,$3,$4,'open',$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT (dedup_key) DO UPDATE SET
		  source=EXCLUDED.source, kind=EXCLUDED.kind, severity=EXCLUDED.severity,
		  title=EXCLUDED.title, message=EXCLUDED.message,
		  client_ip=EXCLUDED.client_ip, client_mac=EXCLUDED.client_mac,
		  domain=EXCLUDED.domain, component=EXCLUDED.component,
		  intel_source=EXCLUDED.intel_source, intel_source_name=EXCLUDED.intel_source_name,
		  count=CASE WHEN alert.status='resolved' THEN EXCLUDED.count
		             ELSE GREATEST(alert.count, EXCLUDED.count) END,
		  detail=EXCLUDED.detail,
		  first_seen=LEAST(alert.first_seen, EXCLUDED.first_seen),
		  last_seen=GREATEST(alert.last_seen, EXCLUDED.last_seen),
		  status=CASE WHEN alert.status='resolved' THEN 'open' ELSE alert.status END,
		  acked_at=CASE WHEN alert.status='resolved' THEN NULL ELSE alert.acked_at END,
		  acked_by=CASE WHEN alert.status='resolved' THEN '' ELSE alert.acked_by END,
		  resolved_at=CASE WHEN alert.status='resolved' THEN NULL ELSE alert.resolved_at END,
		  updated_at=now()
		RETURNING ` + alertColumns
	return scanAlert(r.pool.QueryRow(ctx, q,
		ev.DedupKey, ev.Source, ev.Kind, ev.Severity, ev.Title, ev.Message,
		ev.ClientIP, ev.ClientMAC, ev.Domain, ev.Component,
		ev.IntelSource, ev.IntelSourceName, ev.Count, ev.Detail,
		ev.FirstSeen, ev.LastSeen))
}

// Resolve 系统故障恢复。
func (r *PgRepo) Resolve(ctx context.Context, dedupKey string, at time.Time) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE alert SET status='resolved', resolved_at=$2, updated_at=now()
		 WHERE dedup_key=$1 AND status <> 'resolved'`, dedupKey, at)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ResolveStaleSystem 恢复已不在快照中的系统报警。
func (r *PgRepo) ResolveStaleSystem(ctx context.Context, managedKinds, activeKeys []string, at time.Time) (int, error) {
	if len(managedKinds) == 0 {
		return 0, nil
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE alert SET status='resolved', resolved_at=$3, updated_at=now()
		 WHERE source='system' AND status <> 'resolved'
		   AND kind = ANY($1) AND NOT (dedup_key = ANY($2))`,
		managedKinds, activeKeys, at)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// Purge 删除过期报警。
func (r *PgRepo) Purge(ctx context.Context, before time.Time) (int, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM alert WHERE last_seen < $1`, before)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// ── 内存实现（开发/单测路径）──

// MemRepo 内存仓储。
type MemRepo struct {
	mu    sync.Mutex
	byKey map[string]Alert
}

// NewMemRepo 构造。
func NewMemRepo() *MemRepo { return &MemRepo{byKey: map[string]Alert{}} }

func (r *MemRepo) snapshot(f Filter) []Alert {
	out := make([]Alert, 0, len(r.byKey))
	for _, a := range r.byKey {
		if matchFilter(a, f) {
			out = append(out, a)
		}
	}
	sortAlerts(out)
	return out
}

// List 内存实现。
func (r *MemRepo) List(_ context.Context, f Filter) ([]Alert, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	all := r.snapshot(f)
	total := len(all)
	start := (f.Page - 1) * f.PageSize
	if start > total {
		start = total
	}
	end := start + f.PageSize
	if end > total {
		end = total
	}
	return append([]Alert{}, all[start:end]...), total, nil
}

// Stats 内存实现。
func (r *MemRepo) Stats(_ context.Context, f Filter) (Stats, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	all := r.snapshot(f)
	st := Stats{BySeverity: map[string]int{}, ByKind: map[string]int{},
		ByIntelSource: map[string]int{}, Bucket: f.Bucket}
	doms := map[string]int{}
	clients := map[string]int{}
	bucket := map[string]int{}
	loc := time.UTC
	for _, a := range all {
		st.Total++
		switch a.Status {
		case StatusOpen:
			st.Open++
		case StatusAcked:
			st.Acked++
		case StatusResolved:
			st.Resolved++
		}
		st.BySeverity[a.Severity]++
		st.ByKind[a.Kind]++
		if a.Domain != "" {
			doms[a.Domain] += a.Count
		}
		if a.ClientIP != "" {
			clients[a.ClientIP] += a.Count
		}
		for _, name := range splitIntelSources(a.IntelSource, a.IntelSourceName) {
			st.ByIntelSource[name]++
		}
		key := a.LastSeen.UTC().Format("2006-01-02T15")
		if f.Bucket != "hour" {
			key = a.LastSeen.UTC().Format("2006-01-02")
		}
		bucket[key]++
	}
	topN := func(m map[string]int) []Aggregate {
		out := make([]Aggregate, 0, len(m))
		for k, v := range m {
			out = append(out, Aggregate{Key: k, Count: v})
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Count != out[j].Count {
				return out[i].Count > out[j].Count
			}
			return out[i].Key < out[j].Key
		})
		if len(out) > 10 {
			out = out[:10]
		}
		return out
	}
	st.TopDomains = topN(doms)
	st.TopClients = topN(clients)
	keys := make([]string, 0, len(bucket))
	for k := range bucket {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		layout := "2006-01-02T15"
		if f.Bucket != "hour" {
			layout = "2006-01-02"
		}
		ts, _ := time.ParseInLocation(layout, k, loc)
		st.Trend = append(st.Trend, TrendPoint{TS: ts, Count: bucket[k]})
	}
	return st, nil
}

// Unread 内存实现。
func (r *MemRepo) Unread(_ context.Context, limit int) (Unread, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := Unread{Items: []Alert{}}
	all := make([]Alert, 0, len(r.byKey))
	for _, a := range r.byKey {
		if a.Status == StatusOpen {
			all = append(all, a)
		}
	}
	sortAlerts(all)
	out.Count = len(all)
	if len(all) > limit {
		all = all[:limit]
	}
	out.Items = append(out.Items, all...)
	return out, nil
}

// Ack 内存实现。
func (r *MemRepo) Ack(_ context.Context, ids []string, actor string, at time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	n := 0
	for k, a := range r.byKey {
		if want[a.ID] && a.Status == StatusOpen {
			t := at
			a.Status = StatusAcked
			a.AckedAt = &t
			a.AckedBy = actor
			r.byKey[k] = a
			n++
		}
	}
	return n, nil
}

// AckAll 内存实现。
func (r *MemRepo) AckAll(_ context.Context, actor string, at time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for k, a := range r.byKey {
		if a.Status == StatusOpen {
			t := at
			a.Status = StatusAcked
			a.AckedAt = &t
			a.AckedBy = actor
			r.byKey[k] = a
			n++
		}
	}
	return n, nil
}

// Upsert 内存实现。
func (r *MemRepo) Upsert(_ context.Context, ev Event) (Alert, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.byKey[ev.DedupKey]
	reopened := false
	if !ok {
		a = Alert{
			ID:        guuid.NewString(),
			DedupKey:  ev.DedupKey,
			CreatedAt: ev.FirstSeen,
			Status:    StatusOpen,
		}
	} else if a.Status == StatusResolved {
		a.Status = StatusOpen
		a.AckedAt = nil
		a.AckedBy = ""
		a.ResolvedAt = nil
		reopened = true
	}
	a.Source, a.Kind, a.Severity = ev.Source, ev.Kind, ev.Severity
	a.Title, a.Message, a.Detail = ev.Title, ev.Message, ev.Detail
	a.ClientIP, a.ClientMAC, a.Domain, a.Component = ev.ClientIP, ev.ClientMAC, ev.Domain, ev.Component
	a.IntelSource, a.IntelSourceName = ev.IntelSource, ev.IntelSourceName
	if ok && !reopened {
		a.Count = maxInt(a.Count, ev.Count)
		if ev.FirstSeen.Before(a.FirstSeen) {
			a.FirstSeen = ev.FirstSeen
		}
		if ev.LastSeen.After(a.LastSeen) {
			a.LastSeen = ev.LastSeen
		}
	} else {
		a.Count = ev.Count
		a.FirstSeen, a.LastSeen = ev.FirstSeen, ev.LastSeen
	}
	r.byKey[ev.DedupKey] = a
	return a, nil
}

// Resolve 内存实现。
func (r *MemRepo) Resolve(_ context.Context, dedupKey string, at time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.byKey[dedupKey]
	if !ok || a.Status == StatusResolved {
		return false, nil
	}
	t := at
	a.Status = StatusResolved
	a.ResolvedAt = &t
	r.byKey[dedupKey] = a
	return true, nil
}

// ResolveStaleSystem 内存实现。
func (r *MemRepo) ResolveStaleSystem(_ context.Context, managedKinds, activeKeys []string, at time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	managed := map[string]bool{}
	for _, k := range managedKinds {
		managed[k] = true
	}
	active := map[string]bool{}
	for _, k := range activeKeys {
		active[k] = true
	}
	n := 0
	for k, a := range r.byKey {
		if a.Source == SourceSystem && a.Status != StatusResolved && managed[a.Kind] && !active[a.DedupKey] {
			t := at
			a.Status = StatusResolved
			a.ResolvedAt = &t
			r.byKey[k] = a
			n++
		}
	}
	return n, nil
}

// Purge 内存实现。
func (r *MemRepo) Purge(_ context.Context, before time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for k, a := range r.byKey {
		if a.LastSeen.Before(before) {
			delete(r.byKey, k)
			n++
		}
	}
	return n, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
