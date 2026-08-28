package platform

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// User 用户投影（不含口令散列，对外返回）。
type User struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Roles       []string  `json:"roles"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"createdAt"`
}

// UserRecord 含口令散列的内部行（登录校验专用，禁止出 API）。
type UserRecord struct {
	User
	PasswordHash string
}

// UserCreateInput 新建入参；Password 明文仅在此层内 bcrypt。
type UserCreateInput struct {
	Username    string
	DisplayName string
	Password    string
	Roles       []string
}

// UserUpdateInput PATCH 语义，nil 字段不改。
type UserUpdateInput struct {
	DisplayName *string
	Password    *string
	Roles       *[]string
	Enabled     *bool
}

var ErrUsernameTaken = errors.New("USERNAME_TAKEN")

// UserStore 用户持久化抽象（PG/内存双实现，沿库内 Store 惯例）。
type UserStore interface {
	List(ctx context.Context) ([]User, error)
	GetByID(ctx context.Context, id string) (UserRecord, bool, error)
	GetByUsername(ctx context.Context, username string) (UserRecord, bool, error)
	Create(ctx context.Context, in UserCreateInput) (User, error)
	Update(ctx context.Context, id string, in UserUpdateInput) (User, error)
	Delete(ctx context.Context, id string) error
	Count(ctx context.Context) (int, error)
}

// HashPassword bcrypt 落库（DefaultCost=10）。
func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

// CheckPassword 常量时间散列比对。
func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// EnsureBootstrap 空表时播种初始 admin（口令语义沿用 M5-001，升级兼容）。
func EnsureBootstrap(ctx context.Context, s UserStore) error {
	n, err := s.Count(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err = s.Create(ctx, UserCreateInput{
		Username: pocUsername, DisplayName: "管理员", Password: pocPassword(), Roles: []string{"admin"},
	})
	return err
}

// normalizeRoles 角色白名单收敛（未知角色丢弃；空则 user 只读兜底）。
func normalizeRoles(rs []string) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		r = strings.TrimSpace(r)
		if r == "admin" || r == "user" {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		out = []string{"user"}
	}
	return out
}

// ── 内存实现（PoC/单测） ──────────────────────────────

// MemUserStore 以用户名为主键的内存实现。
type MemUserStore struct {
	mu sync.RWMutex
	m  map[string]UserRecord
}

func NewMemUserStore() *MemUserStore { return &MemUserStore{m: map[string]UserRecord{}} }

func (s *MemUserStore) List(_ context.Context) ([]User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]User, 0, len(s.m))
	for _, r := range s.m {
		out = append(out, r.User)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out, nil
}

func (s *MemUserStore) GetByID(_ context.Context, id string) (UserRecord, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.m {
		if r.ID == id {
			return r, true, nil
		}
	}
	return UserRecord{}, false, nil
}

func (s *MemUserStore) GetByUsername(_ context.Context, username string) (UserRecord, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.m[username]
	return r, ok, nil
}

func (s *MemUserStore) Create(_ context.Context, in UserCreateInput) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, taken := s.m[in.Username]; taken {
		return User{}, fmt.Errorf("%w: %s", ErrUsernameTaken, in.Username)
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		return User{}, err
	}
	rec := UserRecord{
		User: User{
			ID: uuid.NewString(), Username: in.Username, DisplayName: in.DisplayName,
			Roles: normalizeRoles(in.Roles), Enabled: true, CreatedAt: timeNowUTC(),
		},
		PasswordHash: hash,
	}
	s.m[in.Username] = rec
	return rec.User, nil
}

func (s *MemUserStore) Update(_ context.Context, id string, in UserUpdateInput) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var found string
	for name, r := range s.m {
		if r.ID == id {
			found = name
			break
		}
	}
	if found == "" {
		return User{}, errors.New("user not found")
	}
	rec := s.m[found]
	if in.DisplayName != nil {
		rec.DisplayName = *in.DisplayName
	}
	if in.Password != nil {
		h, err := HashPassword(*in.Password)
		if err != nil {
			return User{}, err
		}
		rec.PasswordHash = h
	}
	if in.Roles != nil {
		rec.Roles = normalizeRoles(*in.Roles)
	}
	if in.Enabled != nil {
		rec.Enabled = *in.Enabled
	}
	s.m[found] = rec
	return rec.User, nil
}

func (s *MemUserStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, r := range s.m {
		if r.ID == id {
			delete(s.m, name)
			return nil
		}
	}
	return errors.New("user not found")
}

func (s *MemUserStore) Count(_ context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.m), nil
}

// ── PG 实现（迁移 0010 users 表） ──────────────────────

// PgUserStore users 表实现。
type PgUserStore struct{ pool *pgxpool.Pool }

func NewPgUserStore(pool *pgxpool.Pool) *PgUserStore { return &PgUserStore{pool: pool} }

const userCols = `id::text, username, display_name, roles, enabled, created_at`

func (s *PgUserStore) List(ctx context.Context) ([]User, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+userCols+` FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Roles, &u.Enabled, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *PgUserStore) GetByID(ctx context.Context, id string) (UserRecord, bool, error) {
	return s.getBy(ctx, `id::text=$1`, id)
}

func (s *PgUserStore) GetByUsername(ctx context.Context, username string) (UserRecord, bool, error) {
	return s.getBy(ctx, `username=$1`, username)
}

func (s *PgUserStore) getBy(ctx context.Context, cond, arg string) (UserRecord, bool, error) {
	r := UserRecord{}
	err := s.pool.QueryRow(ctx,
		`SELECT `+userCols+`, password_hash FROM users WHERE `+cond, arg).
		Scan(&r.ID, &r.Username, &r.DisplayName, &r.Roles, &r.Enabled, &r.CreatedAt, &r.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return UserRecord{}, false, nil
	}
	if err != nil {
		return UserRecord{}, false, err
	}
	return r, true, nil
}

func (s *PgUserStore) Create(ctx context.Context, in UserCreateInput) (User, error) {
	hash, err := HashPassword(in.Password)
	if err != nil {
		return User{}, err
	}
	u := User{
		Username: in.Username, DisplayName: in.DisplayName,
		Roles: normalizeRoles(in.Roles), Enabled: true, CreatedAt: timeNowUTC(),
	}
	err = s.pool.QueryRow(ctx,
		`INSERT INTO users(username, display_name, password_hash, roles)
		 VALUES($1,$2,$3,$4) RETURNING id::text, created_at`,
		u.Username, u.DisplayName, hash, u.Roles).Scan(&u.ID, &u.CreatedAt)
	if err != nil && strings.Contains(err.Error(), "duplicate key") {
		return User{}, fmt.Errorf("%w: %s", ErrUsernameTaken, in.Username)
	}
	return u, err
}

func (s *PgUserStore) Update(ctx context.Context, id string, in UserUpdateInput) (User, error) {
	cur, ok, err := s.GetByID(ctx, id)
	if err != nil {
		return User{}, err
	}
	if !ok {
		return User{}, errors.New("user not found")
	}
	hash := cur.PasswordHash
	if in.Password != nil {
		if hash, err = HashPassword(*in.Password); err != nil {
			return User{}, err
		}
	}
	displayName := cur.DisplayName
	if in.DisplayName != nil {
		displayName = *in.DisplayName
	}
	roles := cur.Roles
	if in.Roles != nil {
		roles = normalizeRoles(*in.Roles)
	}
	enabled := cur.Enabled
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	var u User
	err = s.pool.QueryRow(ctx,
		`UPDATE users SET display_name=$2, password_hash=$3, roles=$4, enabled=$5, updated_at=now()
		 WHERE id::text=$1 RETURNING `+userCols,
		id, displayName, hash, roles, enabled).
		Scan(&u.ID, &u.Username, &u.DisplayName, &u.Roles, &u.Enabled, &u.CreatedAt)
	return u, err
}

func (s *PgUserStore) Delete(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM users WHERE id::text=$1`, id)
	return err
}

func (s *PgUserStore) Count(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}
