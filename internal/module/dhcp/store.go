// Package dhcp DHCP 选项与类匹配（M2-016，C-02/C-03；§13.4 DHCP 菜单 5/6）。
// Kea option-data（全局标准选项）与 client-classes（类匹配规则）的 CRUD 与投影；
// 下发经 main 注入的 apply 闭包（kea BuildConfigFull + config-set），本包不依赖 kea 引擎。
package dhcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DhcpOption 全局标准选项行（Kea 全局 option-data 投影）。
type DhcpOption struct {
	ID         string `json:"id"`
	OptionCode int    `json:"optionCode"`
	Name       string `json:"name"`
	Data       string `json:"data"`
	Enabled    bool   `json:"enabled"`
}

// ClassOption 类内选项三元组（Kea option-data 的 name/data 形态）。
type ClassOption struct {
	OptionCode int    `json:"optionCode"`
	Name       string `json:"name"`
	Data       string `json:"data"`
}

// DhcpClass 类匹配规则行（Kea client-classes 投影）。
type DhcpClass struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	Test    string        `json:"test"`
	Options []ClassOption `json:"options"`
	Enabled bool          `json:"enabled"`
}

// OptionUpdate PATCH 入参，nil 不改。
type OptionUpdate struct {
	OptionCode *int
	Name       *string
	Data       *string
	Enabled    *bool
}

// ClassUpdate PATCH 入参；name 为 Kea 引用键不可改。
type ClassUpdate struct {
	Test    *string
	Options *[]ClassOption
	Enabled *bool
}

var ErrOptionTaken = errors.New("OPTION_TAKEN")
var ErrClassTaken = errors.New("CLASS_TAKEN")

// Store 持久化抽象（PG/内存双实现，沿库内 Store 惯例）。
type Store interface {
	ListOptions(ctx context.Context) ([]DhcpOption, error)
	CreateOption(ctx context.Context, o DhcpOption) (DhcpOption, error)
	UpdateOption(ctx context.Context, id string, in OptionUpdate) (DhcpOption, error)
	DeleteOption(ctx context.Context, id string) error
	ListClasses(ctx context.Context) ([]DhcpClass, error)
	CreateClass(ctx context.Context, c DhcpClass) (DhcpClass, error)
	UpdateClass(ctx context.Context, id string, in ClassUpdate) (DhcpClass, error)
	DeleteClass(ctx context.Context, id string) error
}

// ── 内存实现（PoC/单测） ──────────────────────────────

// MemStore 内存实现。
type MemStore struct {
	mu      sync.RWMutex
	opts    map[string]DhcpOption // by id
	classes map[string]DhcpClass
}

func NewMemStore() *MemStore {
	return &MemStore{opts: map[string]DhcpOption{}, classes: map[string]DhcpClass{}}
}

func (s *MemStore) ListOptions(_ context.Context) ([]DhcpOption, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]DhcpOption, 0, len(s.opts))
	for _, o := range s.opts {
		out = append(out, o)
	}
	return out, nil
}

func (s *MemStore) CreateOption(_ context.Context, o DhcpOption) (DhcpOption, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.opts {
		if e.OptionCode == o.OptionCode && e.Name == o.Name {
			return DhcpOption{}, fmt.Errorf("%w: %d/%s", ErrOptionTaken, o.OptionCode, o.Name)
		}
	}
	o.ID = uuid.NewString()
	s.opts[o.ID] = o
	return o, nil
}

func (s *MemStore) UpdateOption(_ context.Context, id string, in OptionUpdate) (DhcpOption, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.opts[id]
	if !ok {
		return DhcpOption{}, pgx.ErrNoRows
	}
	if in.OptionCode != nil {
		o.OptionCode = *in.OptionCode
	}
	if in.Name != nil {
		o.Name = *in.Name
	}
	if in.Data != nil {
		o.Data = *in.Data
	}
	if in.Enabled != nil {
		o.Enabled = *in.Enabled
	}
	s.opts[id] = o
	return o, nil
}

func (s *MemStore) DeleteOption(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.opts, id)
	return nil
}

func (s *MemStore) ListClasses(_ context.Context) ([]DhcpClass, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]DhcpClass, 0, len(s.classes))
	for _, c := range s.classes {
		out = append(out, c)
	}
	return out, nil
}

func (s *MemStore) CreateClass(_ context.Context, c DhcpClass) (DhcpClass, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.classes {
		if e.Name == c.Name {
			return DhcpClass{}, fmt.Errorf("%w: %s", ErrClassTaken, c.Name)
		}
	}
	c.ID = uuid.NewString()
	s.classes[c.ID] = c
	return c, nil
}

func (s *MemStore) UpdateClass(_ context.Context, id string, in ClassUpdate) (DhcpClass, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.classes[id]
	if !ok {
		return DhcpClass{}, pgx.ErrNoRows
	}
	if in.Test != nil {
		c.Test = *in.Test
	}
	if in.Options != nil {
		c.Options = *in.Options
	}
	if in.Enabled != nil {
		c.Enabled = *in.Enabled
	}
	s.classes[id] = c
	return c, nil
}

func (s *MemStore) DeleteClass(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.classes, id)
	return nil
}

// ── PG 实现（迁移 0011 dhcp_options/dhcp_classes） ─────────

// PgStore PG 实现。
type PgStore struct{ pool *pgxpool.Pool }

func NewPgStore(pool *pgxpool.Pool) *PgStore { return &PgStore{pool: pool} }

const optionCols = `id::text, option_code, name, data, enabled`

func (s *PgStore) ListOptions(ctx context.Context) ([]DhcpOption, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+optionCols+` FROM dhcp_options ORDER BY option_code, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DhcpOption{}
	for rows.Next() {
		var o DhcpOption
		if err := rows.Scan(&o.ID, &o.OptionCode, &o.Name, &o.Data, &o.Enabled); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *PgStore) CreateOption(ctx context.Context, o DhcpOption) (DhcpOption, error) {
	err := s.pool.QueryRow(ctx,
		`INSERT INTO dhcp_options(option_code, name, data, enabled) VALUES($1,$2,$3,$4) RETURNING id::text`,
		o.OptionCode, o.Name, o.Data, o.Enabled).Scan(&o.ID)
	if err != nil && strings.Contains(err.Error(), "duplicate key") {
		return DhcpOption{}, fmt.Errorf("%w: %d/%s", ErrOptionTaken, o.OptionCode, o.Name)
	}
	return o, err
}

func (s *PgStore) UpdateOption(ctx context.Context, id string, in OptionUpdate) (DhcpOption, error) {
	cur, ok, err := s.getOption(ctx, id)
	if err != nil {
		return DhcpOption{}, err
	}
	if !ok {
		return DhcpOption{}, pgx.ErrNoRows
	}
	if in.OptionCode != nil {
		cur.OptionCode = *in.OptionCode
	}
	if in.Name != nil {
		cur.Name = *in.Name
	}
	if in.Data != nil {
		cur.Data = *in.Data
	}
	if in.Enabled != nil {
		cur.Enabled = *in.Enabled
	}
	var o DhcpOption
	err = s.pool.QueryRow(ctx,
		`UPDATE dhcp_options SET option_code=$2, name=$3, data=$4, enabled=$5 WHERE id::text=$1 RETURNING `+optionCols,
		id, cur.OptionCode, cur.Name, cur.Data, cur.Enabled).
		Scan(&o.ID, &o.OptionCode, &o.Name, &o.Data, &o.Enabled)
	return o, err
}

func (s *PgStore) getOption(ctx context.Context, id string) (DhcpOption, bool, error) {
	var o DhcpOption
	err := s.pool.QueryRow(ctx, `SELECT `+optionCols+` FROM dhcp_options WHERE id::text=$1`, id).
		Scan(&o.ID, &o.OptionCode, &o.Name, &o.Data, &o.Enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return DhcpOption{}, false, nil
	}
	if err != nil {
		return DhcpOption{}, false, err
	}
	return o, true, nil
}

func (s *PgStore) DeleteOption(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM dhcp_options WHERE id::text=$1`, id)
	return err
}

const classCols = `id::text, name, test, options, enabled`

func (s *PgStore) ListClasses(ctx context.Context) ([]DhcpClass, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+classCols+` FROM dhcp_classes ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DhcpClass{}
	for rows.Next() {
		c, scanErr := scanClass(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// classRowSource 抽象行扫描（rows 与 QueryRow 复用）。
type classRowSource interface{ Scan(dest ...any) error }

func scanClass(r classRowSource) (DhcpClass, error) {
	var c DhcpClass
	var raw []byte
	if err := r.Scan(&c.ID, &c.Name, &c.Test, &raw, &c.Enabled); err != nil {
		return DhcpClass{}, err
	}
	c.Options = []ClassOption{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &c.Options); err != nil {
			return DhcpClass{}, err
		}
	}
	return c, nil
}

func (s *PgStore) CreateClass(ctx context.Context, c DhcpClass) (DhcpClass, error) {
	raw, err := json.Marshal(c.Options)
	if err != nil {
		return DhcpClass{}, err
	}
	err = s.pool.QueryRow(ctx,
		`INSERT INTO dhcp_classes(name, test, options, enabled) VALUES($1,$2,$3,$4) RETURNING id::text`,
		c.Name, c.Test, raw, c.Enabled).Scan(&c.ID)
	if err != nil && strings.Contains(err.Error(), "duplicate key") {
		return DhcpClass{}, fmt.Errorf("%w: %s", ErrClassTaken, c.Name)
	}
	return c, err
}

func (s *PgStore) UpdateClass(ctx context.Context, id string, in ClassUpdate) (DhcpClass, error) {
	cur, ok, err := s.getClass(ctx, id)
	if err != nil {
		return DhcpClass{}, err
	}
	if !ok {
		return DhcpClass{}, pgx.ErrNoRows
	}
	if in.Test != nil {
		cur.Test = *in.Test
	}
	if in.Options != nil {
		cur.Options = *in.Options
	}
	if in.Enabled != nil {
		cur.Enabled = *in.Enabled
	}
	raw, err := json.Marshal(cur.Options)
	if err != nil {
		return DhcpClass{}, err
	}
	var c DhcpClass
	err = s.pool.QueryRow(ctx,
		`UPDATE dhcp_classes SET test=$2, options=$3, enabled=$4, updated_at=now() WHERE id::text=$1 RETURNING `+classCols,
		id, cur.Test, raw, cur.Enabled).Scan(&c.ID, &c.Name, &c.Test, &raw, &c.Enabled)
	if err != nil {
		return DhcpClass{}, err
	}
	c.Options = []ClassOption{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &c.Options); err != nil {
			return DhcpClass{}, err
		}
	}
	return c, nil
}

func (s *PgStore) getClass(ctx context.Context, id string) (DhcpClass, bool, error) {
	var raw []byte
	var c DhcpClass
	err := s.pool.QueryRow(ctx, `SELECT `+classCols+` FROM dhcp_classes WHERE id::text=$1`, id).
		Scan(&c.ID, &c.Name, &c.Test, &raw, &c.Enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return DhcpClass{}, false, nil
	}
	if err != nil {
		return DhcpClass{}, false, err
	}
	c.Options = []ClassOption{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &c.Options); err != nil {
			return DhcpClass{}, false, err
		}
	}
	return c, true, nil
}

func (s *PgStore) DeleteClass(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM dhcp_classes WHERE id::text=$1`, id)
	return err
}
