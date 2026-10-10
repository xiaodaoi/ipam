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

const auditCols = `id, extract(epoch from ts)*1000, actor_type, actor, token_sub,
  method, path, action, resource, status, detail`

// Append 写入一条审计（ID 由库生成；TS 为零值时回填当前时刻）。
func (s *PgAuditStore) Append(ctx context.Context, e AuditEntry) (AuditEntry, error) {
	actor := e.Actor
	if actor == "" {
		actor = "anonymous"
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO operation_audit(actor_type, actor, token_sub, method, path, action, resource, status, detail)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		string(e.ActorType), actor, e.TokenSub,
		e.Method, e.Path, e.Action, e.Resource, e.Status, e.Detail,
	).Scan(&e.ID)
	if err == nil {
		if e.TS.IsZero() {
			e.TS = time.Now().UTC()
		}
		s.maybeCleanup(ctx)
	}
	return e, err
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
		var tsMillis float64
		var actor, atype string
		if err := rows.Scan(&r.ID, &tsMillis, &atype, &actor, &r.TokenSub,
			&r.Method, &r.Path, &r.Action, &r.Resource, &r.Status, &r.Detail); err != nil {
			return AuditPage{}, err
		}
		r.TS = time.UnixMilli(int64(tsMillis)).UTC()
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
