// Package dualstack 双栈绑定模板管理（M2-012，§4.3 多池对；PG prefix_template）。
// CRUD 供「双栈管理」页消费；daemon 侧通过 All() 投影投喂 coherence 联动匹配。
package dualstack

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/xiaodaoi/ipam/internal/module/coherence"
)

// Template 模板行（PG prefix_template 投影）。
type Template struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	V4Cidr     string `json:"ipv4Cidr"`
	V6Prefix   string `json:"ipv6Prefix"`
	Encoding   string `json:"encoding"`
	Expr       string `json:"expr"`
	DnsSync    bool   `json:"dnsSync"`
	GraceHours int    `json:"graceHours"`
	Enabled    bool   `json:"enabled"`
}

// Store 持久化抽象（PG/内存双实现，沿用模块惯例）。
type Store interface {
	List(ctx context.Context) ([]Template, error)
	Create(ctx context.Context, t Template) (Template, error)
	Delete(ctx context.Context, id string) error
}

// NewMemStore 内存实现（PoC/单测）。
type MemStore struct {
	rows []Template
}

func NewMemStore() *MemStore { return &MemStore{} }

func (m *MemStore) List(_ context.Context) ([]Template, error) { return m.rows, nil }

func (m *MemStore) Create(_ context.Context, t Template) (Template, error) {
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	t.Enabled = true
	m.rows = append(m.rows, t)
	return t, nil
}

func (m *MemStore) Delete(_ context.Context, id string) error {
	out := m.rows[:0]
	for _, r := range m.rows {
		if r.ID != id {
			out = append(out, r)
		}
	}
	m.rows = out
	return nil
}

// CoherenceTemplates 转换为 coherence 联动匹配投影（daemon SetTemplateAll 同构）。
func CoherenceTemplates(ts []Template) []coherence.Template {
	out := make([]coherence.Template, 0, len(ts))
	for _, t := range ts {
		if !t.Enabled {
			continue
		}
		out = append(out, coherence.Template{
			ID: t.ID, V4Cidr: t.V4Cidr, Prefix: t.V6Prefix, Expr: t.Expr,
		})
	}
	return out
}

// PgStore PG 实现（迁移 0001 的 prefix_template 表）。
type PgStore struct{ pool *pgxpool.Pool }

func NewPgStore(pool *pgxpool.Pool) *PgStore { return &PgStore{pool: pool} }

const tplCols = `id::text, name, ipv4_cidr::text, ipv6_prefix::text, encoding, expr, dns_sync, grace_hours, enabled`

func (s *PgStore) List(ctx context.Context) ([]Template, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+tplCols+` FROM prefix_template ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Template{}
	for rows.Next() {
		var t Template
		if err := rows.Scan(&t.ID, &t.Name, &t.V4Cidr, &t.V6Prefix, &t.Encoding,
			&t.Expr, &t.DnsSync, &t.GraceHours, &t.Enabled); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *PgStore) Create(ctx context.Context, t Template) (Template, error) {
	return t, s.pool.QueryRow(ctx,
		`INSERT INTO prefix_template(name, ipv4_cidr, ipv6_prefix, encoding, expr, dns_sync, grace_hours, enabled)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id::text`,
		t.Name, t.V4Cidr, t.V6Prefix, strings.ToUpper(t.Encoding), t.Expr,
		t.DnsSync, t.GraceHours, t.Enabled).Scan(&t.ID)
}

func (s *PgStore) Delete(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM prefix_template WHERE id=$1`, id)
	return err
}
