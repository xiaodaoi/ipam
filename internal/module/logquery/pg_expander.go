package logquery

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PgOrgExpander 组织展开（PG 侧，§13.4 关联链）：
// 子树展开依赖 org_group.path 物化路径；CIDR 取自 subnet.cidr；MAC 取自 asset。
type PgOrgExpander struct {
	pool *pgxpool.Pool
}

func NewPgOrgExpander(pool *pgxpool.Pool) *PgOrgExpander { return &PgOrgExpander{pool: pool} }

// Expand 展开 orgID 子树：全部 CIDR ∪ 组内资产 MAC。
func (e *PgOrgExpander) Expand(ctx context.Context, orgID string) (OrgScope, error) {
	var scope OrgScope

	var path string
	if err := e.pool.QueryRow(ctx,
		`SELECT path FROM org_group WHERE id=$1`, orgID).Scan(&path); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return scope, ErrOrgNotFound
		}
		return scope, err
	}

	rows, err := e.pool.Query(ctx,
		`SELECT cidr::text FROM subnet WHERE org_id IN (
		   SELECT id FROM org_group WHERE path LIKE $1 || '%')`, path)
	if err != nil {
		return scope, err
	}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			rows.Close()
			return scope, err
		}
		scope.CIDRs = append(scope.CIDRs, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return scope, err
	}

	macRows, err := e.pool.Query(ctx,
		`SELECT mac FROM asset WHERE org_id IN (
		   SELECT id FROM org_group WHERE path LIKE $1 || '%')`, path)
	if err != nil {
		return scope, err
	}
	for macRows.Next() {
		var mac string
		if err := macRows.Scan(&mac); err != nil {
			macRows.Close()
			return scope, err
		}
		scope.MACs = append(scope.MACs, NormalizeMAC(mac))
	}
	macRows.Close()
	if err := macRows.Err(); err != nil {
		return scope, err
	}
	return scope, nil
}
