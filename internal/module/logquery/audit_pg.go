package logquery

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PgAuditStore PostgreSQL 实现（迁移 0009）。
type PgAuditStore struct {
	pool *pgxpool.Pool
	// lastCleanup 上次保留清理时刻（UnixNano，0=从未清理）；仅经 atomic 访问。
	lastCleanup atomic.Int64
}

// 审计保留策略（M5-032）：与产品日志保留一致 180 天。惰性触发——Append 顺带检查，
// 间隔满 1h 才执行一次 DELETE，无需 main.go 额外接线。
const (
	auditRetentionDays   = 180
	auditCleanupInterval = time.Hour
	auditCleanupTimeout  = 30 * time.Second
)

// auditCleanupSQL 删除保留期之外的审计行（走 idx_operation_audit_ts 索引范围扫描）。
const auditCleanupSQL = `DELETE FROM operation_audit WHERE ts < $1`

func NewPgAuditStore(pool *pgxpool.Pool) *PgAuditStore { return &PgAuditStore{pool: pool} }

// auditCutoff 保留期截止时刻：ts 早于该时刻的行将被清理。
func auditCutoff(now time.Time) time.Time { return now.AddDate(0, 0, -auditRetentionDays).UTC() }

// auditCleanupDue 判断是否到达清理时点（last 零值表示从未清理，立即到期）。
func auditCleanupDue(now, last time.Time) bool {
	return last.IsZero() || now.Sub(last) >= auditCleanupInterval
}

// maybeCleanup 惰性保留清理：每 auditCleanupInterval 至多一次，并发 Append 只放行一个执行者；
// 失败仅记日志，不影响审计写入路径。
func (s *PgAuditStore) maybeCleanup(ctx context.Context) {
	now := time.Now().UTC()
	lastUnix := s.lastCleanup.Load()
	var last time.Time
	if lastUnix != 0 {
		last = time.Unix(0, lastUnix).UTC()
	}
	if !auditCleanupDue(now, last) {
		return
	}
	if !s.lastCleanup.CompareAndSwap(lastUnix, now.UnixNano()) {
		return // 另一并发 Append 已接手本轮清理
	}
	cctx, cancel := context.WithTimeout(ctx, auditCleanupTimeout)
	defer cancel()
	if _, err := s.pool.Exec(cctx, auditCleanupSQL, auditCutoff(now)); err != nil {
		log.Printf("logquery: audit retention cleanup: %v", err)
	}
}

const auditCols = `id, ts, actor_type, actor, token_sub,
  method, path, action, resource, status, detail, payload, prev_hash, chain_hash`

// Append 写入一条审计（ID 由库生成；TS 为零值时回填当前时刻）。
func (s *PgAuditStore) Append(ctx context.Context, e AuditEntry) (AuditEntry, error) {
	actor := e.Actor
	if actor == "" {
		actor = "anonymous"
	}
	if e.TS.IsZero() {
		e.TS = time.Now().UTC()
	}
	e.TS = e.TS.UTC().Truncate(time.Microsecond)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return e, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// 链式写入串行化：同一时刻只允许一个 Append 读取上一条哈希并插入。
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(918273645)`); err != nil {
		return e, err
	}
	var prev string
	if err := tx.QueryRow(ctx, `SELECT coalesce((SELECT chain_hash FROM operation_audit ORDER BY id DESC LIMIT 1),'')`).Scan(&prev); err != nil {
		return e, err
	}
	var id int64
	if err := tx.QueryRow(ctx, `SELECT nextval('operation_audit_id_seq')`).Scan(&id); err != nil {
		return e, err
	}
	e.ID = id
	e.PrevHash = prev
	e.ChainHash = auditChainHash(e, prev)
	if _, err := tx.Exec(ctx,
		`INSERT INTO operation_audit(id, ts, actor_type, actor, token_sub, method, path, action, resource, status, detail, payload, prev_hash, chain_hash)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		e.ID, e.TS, string(e.ActorType), actor, e.TokenSub,
		e.Method, e.Path, e.Action, e.Resource, e.Status, e.Detail, e.Payload, e.PrevHash, e.ChainHash,
	); err != nil {
		return e, err
	}
	if err := tx.Commit(ctx); err != nil {
		return e, err
	}
	s.maybeCleanup(ctx)
	return e, nil
}

// Query 组合过滤 + (ts,id) 元组游标分页（参数闭包保证占位符序号与 args 同步推进）。
func (s *PgAuditStore) Query(ctx context.Context, f AuditFilter) (AuditPage, error) {
	var where []string
	var args []any
	param := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}

	where = append(where, "ts >= "+param(f.From.UTC()))
	to := f.To
	if to.IsZero() {
		to = time.Now().UTC()
	}
	where = append(where, "ts <= "+param(to.UTC()))
	if f.ActorType != "" {
		where = append(where, "actor_type = "+param(string(f.ActorType)))
	}
	if f.Action != "" {
		where = append(where, "action = "+param(f.Action))
	}
	if f.Q != "" {
		like := "%" + strings.ToLower(f.Q) + "%"
		where = append(where, "(lower(resource) LIKE "+param(like)+" OR lower(path) LIKE "+param(like)+")")
	}
	if f.Cursor != "" {
		if cts, cid := ParseAuditCursor(f.Cursor); !cts.IsZero() {
			where = append(where, "(ts < "+param(cts.UTC())+" OR (ts = "+param(cts.UTC())+" AND id < "+param(cid)+"))")
		}
	}

	pageSize := f.PageSize
	if pageSize <= 0 {
		pageSize = DefaultPage
	}
	wsql := strings.Join(where, " AND ")

	var total int
	if err := s.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT count(*) FROM operation_audit WHERE %s`, wsql), args...).Scan(&total); err != nil {
		return AuditPage{}, err
	}

	sql := fmt.Sprintf(`SELECT %s FROM operation_audit WHERE %s ORDER BY ts DESC, id DESC LIMIT %s`,
		auditCols, wsql, param(pageSize+1))
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return AuditPage{}, err
	}
	defer rows.Close()

	items := make([]AuditEntry, 0, pageSize)
	for rows.Next() {
		var r AuditEntry
		var actor, atype string
		if err := rows.Scan(&r.ID, &r.TS, &atype, &actor, &r.TokenSub,
			&r.Method, &r.Path, &r.Action, &r.Resource, &r.Status, &r.Detail,
			&r.Payload, &r.PrevHash, &r.ChainHash); err != nil {
			return AuditPage{}, err
		}
		r.Actor, r.ActorType = actor, ActorType(atype)
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return AuditPage{}, err
	}
	next := ""
	if len(items) > pageSize {
		next = EncodeAuditCursor(items[pageSize-1])
		items = items[:pageSize]
	}
	return AuditPage{Items: items, NextCursor: next, Total: total}, nil
}

// Verify 校验时间窗内带链哈希记录的完整性与相邻链接。
func (s *PgAuditStore) Verify(ctx context.Context, from, to time.Time) (int, int64, error) {
	if to.IsZero() {
		to = time.Now().UTC()
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+auditCols+` FROM operation_audit
		  WHERE ts >= $1 AND ts <= $2 AND chain_hash <> '' ORDER BY id ASC LIMIT 10000`,
		from.UTC(), to.UTC())
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	var checked int
	var prev string
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.TS, &e.ActorType, &e.Actor, &e.TokenSub,
			&e.Method, &e.Path, &e.Action, &e.Resource, &e.Status, &e.Detail,
			&e.Payload, &e.PrevHash, &e.ChainHash); err != nil {
			return checked, 0, err
		}
		if e.ChainHash != auditChainHash(e, e.PrevHash) {
			return checked, e.ID, nil
		}
		if prev != "" && e.PrevHash != prev {
			return checked, e.ID, nil
		}
		prev = e.ChainHash
		checked++
	}
	return checked, 0, rows.Err()
}
