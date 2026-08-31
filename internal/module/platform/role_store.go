package platform

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Role 角色（M2-035）：permissions 为权限点集合（域:read|write）。
type Role struct {
	Name        string
	Permissions []string
	Builtin     bool
}

// RoleStore 角色存储抽象（Mem 测试/Pg 生产）。
type RoleStore interface {
	List(ctx context.Context) ([]Role, error)
	Get(ctx context.Context, name string) (Role, bool, error)
	Create(ctx context.Context, r Role) error
	Update(ctx context.Context, name string, permissions []string) error
	Delete(ctx context.Context, name string) error
}

// MemRoleStore 内存实现（PoC/单测）。
type MemRoleStore struct {
	mu sync.Mutex
	m  map[string]Role
}

func NewMemRoleStore() *MemRoleStore {
	return &MemRoleStore{m: map[string]Role{}}
}

func (s *MemRoleStore) List(_ context.Context) ([]Role, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Role, 0, len(s.m))
	for _, r := range s.m {
		out = append(out, r)
	}
	return out, nil
}

func (s *MemRoleStore) Get(_ context.Context, name string) (Role, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.m[name]
	return r, ok, nil
}

func (s *MemRoleStore) Create(_ context.Context, r Role) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[r.Name]; ok {
		return ErrRoleTaken
	}
	s.m[r.Name] = r
	RegisterRole(r.Name)
	return nil
}

func (s *MemRoleStore) Update(_ context.Context, name string, permissions []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.m[name]
	if !ok {
		return ErrRoleNotFound
	}
	r.Permissions = permissions
	s.m[name] = r
	return nil
}

func (s *MemRoleStore) Delete(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, name)
	return nil
}

// PgRoleStore PG 实现（roles 表，迁移 0016）。
type PgRoleStore struct {
	pool *pgxpool.Pool
}

func NewPgRoleStore(pool *pgxpool.Pool) *PgRoleStore { return &PgRoleStore{pool: pool} }

func (s *PgRoleStore) List(ctx context.Context) ([]Role, error) {
	rows, err := s.pool.Query(ctx, `SELECT name, permissions, builtin FROM roles ORDER BY builtin DESC, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Role, 0)
	for rows.Next() {
		var r Role
		var perms string
		if err := rows.Scan(&r.Name, &perms, &r.Builtin); err != nil {
			return nil, err
		}
		r.Permissions = parsePerms(perms)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *PgRoleStore) Get(ctx context.Context, name string) (Role, bool, error) {
	var r Role
	var perms string
	err := s.pool.QueryRow(ctx, `SELECT name, permissions, builtin FROM roles WHERE name = $1`, name).
		Scan(&r.Name, &perms, &r.Builtin)
	if err != nil {
		return Role{}, false, nil
	}
	r.Permissions = parsePerms(perms)
	return r, true, nil
}

func (s *PgRoleStore) Create(ctx context.Context, r Role) error {
	perms, _ := marshalPerms(r.Permissions)
	_, err := s.pool.Exec(ctx, `INSERT INTO roles (name, permissions, builtin) VALUES ($1, $2, false)`, r.Name, perms)
	if err == nil {
		RegisterRole(r.Name)
	}
	return err
}

func (s *PgRoleStore) Update(ctx context.Context, name string, permissions []string) error {
	perms, _ := marshalPerms(permissions)
	tag, err := s.pool.Exec(ctx, `UPDATE roles SET permissions = $2 WHERE name = $1 AND builtin = false`, name, perms)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrRoleNotFound
	}
	return nil
}

func (s *PgRoleStore) Delete(ctx context.Context, name string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM roles WHERE name = $1 AND builtin = false`, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrRoleNotFound
	}
	return nil
}

func parsePerms(s string) []string {
	var out []string
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

func marshalPerms(ps []string) (string, error) {
	b, err := json.Marshal(ps)
	return string(b), err
}
