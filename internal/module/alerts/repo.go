package alerts

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const alertColumns = `id, dedup_key, fingerprint, source, kind, severity, status, title, message,
  client_ip, client_mac, domain, component, intel_source, intel_source_name,
  alert_class, notify, policy_id, group_key, count, detail, window_start,
  notify_count, last_notified_at, suppressed_by, silence_id, decision,
  repeat_after_sec, resolve_after_sec, first_seen, last_seen, acked_at, acked_by,
  resolved_at, created_at`

// justNotifiedExpr 判断本次 upsert 是否刚刚写入过 last_notified_at。
// 必须 COALESCE：新告警（notify=false）的 last_notified_at 为 NULL，
// `NULL = now()` 会得到 NULL，直接扫进 bool 会报错并中断整轮采集。
const justNotifiedExpr = `COALESCE(last_notified_at = now(), false)`

// PgRepo PostgreSQL 实现（迁移 0054）。
type PgRepo struct{ pool *pgxpool.Pool }

// NewPgRepo 构造。
func NewPgRepo(pool *pgxpool.Pool) *PgRepo { return &PgRepo{pool: pool} }

func likePattern(q string) string {
	q = strings.ReplaceAll(q, `\`, `\\`)
	q = strings.ReplaceAll(q, `%`, `\%`)
	q = strings.ReplaceAll(q, `_`, `\_`)
	return "%" + q + "%"
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nilIfZero(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func jsonOrEmpty(s string) []byte {
	if strings.TrimSpace(s) == "" {
		return []byte("{}")
	}
	return []byte(s)
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
	add("alert_class = $%d", f.AlertClass)
	if f.Notify != nil {
		args = append(args, *f.Notify)
		conds = append(conds, fmt.Sprintf("notify = $%d", len(args)))
	}
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
	return scanAlertExtra(sc, false)
}

func scanAlertExtra(sc rowScanner, withJustified bool) (Alert, error) {
	var (
		a                         Alert
		count, notifyCount        int64
		policyID, silenceID       *string
		windowStart, lastNotified *time.Time
		ackedAt, resolvedAt       *time.Time
		decision                  []byte
		justified                 bool
	)
	dest := []any{
		&a.ID, &a.DedupKey, &a.Fingerprint, &a.Source, &a.Kind, &a.Severity, &a.Status,
		&a.Title, &a.Message, &a.ClientIP, &a.ClientMAC, &a.Domain, &a.Component,
		&a.IntelSource, &a.IntelSourceName, &a.AlertClass, &a.Notify, &policyID, &a.GroupKey,
		&count, &a.Detail, &windowStart, &notifyCount, &lastNotified, &a.SuppressedBy, &silenceID,
		&decision, &a.RepeatAfterSec, &a.ResolveAfterSec, &a.FirstSeen, &a.LastSeen,
		&ackedAt, &a.AckedBy, &resolvedAt, &a.CreatedAt,
	}
	if withJustified {
		dest = append(dest, &justified)
	}
	if err := sc.Scan(dest...); err != nil {
		return Alert{}, err
	}
	a.Count = int(count)
	a.NotifyCount = int(notifyCount)
	a.Decision = string(decision)
	if policyID != nil {
		a.PolicyID = *policyID
	}
	if silenceID != nil {
		a.SilenceID = *silenceID
	}
	if windowStart != nil {
		a.WindowStart = *windowStart
	}
	a.LastNotifiedAt = lastNotified
	a.AckedAt = ackedAt
	a.ResolvedAt = resolvedAt
	a.JustNotified = withJustified && justified
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
		"SELECT status, severity, kind, notify, (suppressed_by <> '') AS suppressed, count(*) FROM alert WHERE "+
			where+" GROUP BY 1,2,3,4,5", args...)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var status, severity, kind string
		var notify, suppressed bool
		var n int64
		if err := rows.Scan(&status, &severity, &kind, &notify, &suppressed, &n); err != nil {
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
		if notify {
			st.Notified += int(n)
		} else {
			st.Archived += int(n)
		}
		if suppressed {
			st.Suppressed += int(n)
		}
		if severity == SeverityCritical && !notify {
			st.CriticalSuppressed += int(n)
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

	// 按来源/组件统计（降噪总览用）
	brs, err := r.pool.Query(ctx,
		"SELECT COALESCE(NULLIF(intel_source_name, ''), NULLIF(component, ''), source) AS k,"+
			" count(*) AS recorded, count(*) FILTER (WHERE notify) AS notified"+
			" FROM alert WHERE "+where+" GROUP BY k ORDER BY recorded DESC, k LIMIT 10", args...)
	if err != nil {
		return st, err
	}
	for brs.Next() {
		var s SourceStat
		var recorded, notified int64
		if err := brs.Scan(&s.Key, &recorded, &notified); err != nil {
			brs.Close()
			return st, err
		}
		s.Recorded, s.Notified = int(recorded), int(notified)
		st.BySource = append(st.BySource, s)
	}
	brs.Close()
	if err := brs.Err(); err != nil {
		return st, err
	}

	bucket := f.Bucket
	if bucket != "hour" {
		bucket = "day"
	}
	targs := append(append([]any{}, args...), bucket)
	trs, err := r.pool.Query(ctx,
		"SELECT date_trunc($"+strconv.Itoa(len(targs))+", last_seen) AS b, count(*),"+
			" count(*) FILTER (WHERE notify) FROM alert WHERE "+where+" GROUP BY b ORDER BY b", targs...)
	if err != nil {
		return st, err
	}
	defer trs.Close()
	st.Trend = make([]TrendPoint, 0, 32)
	st.NotifiedTrend = make([]TrendPoint, 0, 32)
	for trs.Next() {
		var p TrendPoint
		var n, notified int64
		if err := trs.Scan(&p.TS, &n, &notified); err != nil {
			return st, err
		}
		p.TS = p.TS.UTC()
		p.Count = int(n)
		st.Trend = append(st.Trend, p)
		st.NotifiedTrend = append(st.NotifiedTrend, TrendPoint{TS: p.TS, Count: int(notified)})
	}
	return st, trs.Err()
}

// Unread 未确认总数 + 最近 N 条（仅 notify=true，critical 优先）。
func (r *PgRepo) Unread(ctx context.Context, limit int) (Unread, error) {
	var out Unread
	if err := r.pool.QueryRow(ctx,
		"SELECT count(*) FROM alert WHERE status='open' AND notify=true").Scan(&out.Count); err != nil {
		return out, err
	}
	rows, err := r.pool.Query(ctx,
		"SELECT "+alertColumns+" FROM alert WHERE status='open' AND notify=true"+
			" ORDER BY CASE severity WHEN 'critical' THEN 0 WHEN 'warning' THEN 1 ELSE 2 END,"+
			" last_seen DESC, id LIMIT $1", limit)
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

// upsertReopenExpr 判断已确认报警是否应重新打开（重复提醒/数量激增）。
const upsertReopenExpr = `(alert.status='resolved') OR (alert.status='acked' AND (
  (alert.repeat_after_sec > 0 AND EXCLUDED.last_seen >= alert.last_seen + make_interval(secs => alert.repeat_after_sec))
  OR (alert.count > 0 AND EXCLUDED.count >= alert.count * 10)))`

// Upsert 按 dedup_key 合并：稳定指纹 + 冷却/重复提醒，重复扫描不回退计数。
func (r *PgRepo) Upsert(ctx context.Context, ev Event) (Alert, error) {
	q := `INSERT INTO alert(dedup_key, fingerprint, source, kind, severity, status, title, message,
		client_ip, client_mac, domain, component, intel_source, intel_source_name,
		alert_class, notify, policy_id, group_key, count, detail, window_start,
		notify_count, last_notified_at, suppressed_by, silence_id, decision,
		repeat_after_sec, resolve_after_sec, first_seen, last_seen)
		VALUES($1,$2,$3,$4,$5,'open',$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,
		CASE WHEN $15 THEN 1 ELSE 0 END,
		CASE WHEN $15 THEN now() ELSE NULL END,
		$21,$22,$23,$24,$25,$26,$27)
		ON CONFLICT (dedup_key) DO UPDATE SET
		  fingerprint=EXCLUDED.fingerprint, source=EXCLUDED.source, kind=EXCLUDED.kind,
		  severity=EXCLUDED.severity, title=EXCLUDED.title, message=EXCLUDED.message,
		  client_ip=EXCLUDED.client_ip, client_mac=EXCLUDED.client_mac, domain=EXCLUDED.domain,
		  component=EXCLUDED.component, intel_source=EXCLUDED.intel_source,
		  intel_source_name=EXCLUDED.intel_source_name, alert_class=EXCLUDED.alert_class,
		  policy_id=EXCLUDED.policy_id, group_key=EXCLUDED.group_key, detail=EXCLUDED.detail,
		  decision=EXCLUDED.decision,
		  repeat_after_sec=EXCLUDED.repeat_after_sec, resolve_after_sec=EXCLUDED.resolve_after_sec,
		  count=CASE WHEN alert.status='resolved' THEN EXCLUDED.count
		             ELSE GREATEST(alert.count, EXCLUDED.count) END,
		  first_seen=LEAST(alert.first_seen, EXCLUDED.first_seen),
		  last_seen=GREATEST(alert.last_seen, EXCLUDED.last_seen),
		  status=CASE WHEN ` + upsertReopenExpr + ` THEN 'open' ELSE alert.status END,
		  acked_at=CASE WHEN ` + upsertReopenExpr + ` THEN NULL ELSE alert.acked_at END,
		  acked_by=CASE WHEN ` + upsertReopenExpr + ` THEN '' ELSE alert.acked_by END,
		  resolved_at=CASE WHEN ` + upsertReopenExpr + ` THEN NULL ELSE alert.resolved_at END,
		  notify=CASE WHEN ` + upsertReopenExpr + ` THEN EXCLUDED.notify ELSE alert.notify END,
		  notify_count=CASE WHEN ` + upsertReopenExpr + ` AND EXCLUDED.notify
		                    THEN alert.notify_count + 1 ELSE alert.notify_count END,
		  last_notified_at=CASE WHEN ` + upsertReopenExpr + ` AND EXCLUDED.notify
		                    THEN now() ELSE alert.last_notified_at END,
		  window_start=CASE WHEN ` + upsertReopenExpr + ` THEN EXCLUDED.window_start
		                    ELSE LEAST(alert.window_start, EXCLUDED.window_start) END,
		  suppressed_by=EXCLUDED.suppressed_by, silence_id=EXCLUDED.silence_id,
		  updated_at=now()
		RETURNING ` + alertColumns + `, ` + justNotifiedExpr + ` AS just_notified`
	return scanAlertExtra(r.pool.QueryRow(ctx, q,
		ev.DedupKey, ev.Fingerprint, ev.Source, ev.Kind, ev.Severity, ev.Title, ev.Message,
		ev.ClientIP, ev.ClientMAC, ev.Domain, ev.Component, ev.IntelSource, ev.IntelSourceName,
		ev.AlertClass, ev.Notify, nilIfEmpty(ev.PolicyID), ev.GroupKey, ev.Count, ev.Detail,
		nilIfZero(ev.WindowStart), ev.SuppressedBy, nilIfEmpty(ev.SilenceID),
		jsonOrEmpty(ev.Decision), ev.RepeatAfterSec, ev.ResolveAfterSec, ev.FirstSeen, ev.LastSeen), true)
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

// ResolveStaleSystem 恢复已不在当前故障快照中的系统报警。
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

// AutoResolveDNS 按每条报警的 resolve_after_sec 自动恢复长时间无命中的 DNS 报警。
func (r *PgRepo) AutoResolveDNS(ctx context.Context, now time.Time) (int, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE alert SET status='resolved', resolved_at=$1, updated_at=now()
		 WHERE source='dns' AND status <> 'resolved' AND resolve_after_sec > 0
		   AND last_seen < $1 - make_interval(secs => resolve_after_sec)`, now)
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
