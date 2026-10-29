package platform

import (
	"context"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
)

// SecuritySettings 账号安全策略（M6-001）。单行表，默认关闭强制改密。
type SecuritySettings struct {
	ForcePasswordChange bool
	UpdatedAt           time.Time
}

// SecuritySettingsStore 账号安全策略持久化抽象（PG/内存双实现）。
type SecuritySettingsStore interface {
	Get(ctx context.Context) (SecuritySettings, error)
	Save(ctx context.Context, forcePasswordChange bool) (SecuritySettings, error)
}

// MemSecuritySettingsStore 内存实现（测试/纯内存 PoC）。
type MemSecuritySettingsStore struct{ s SecuritySettings }

func NewMemSecuritySettingsStore() *MemSecuritySettingsStore {
	return &MemSecuritySettingsStore{}
}

func (m *MemSecuritySettingsStore) Get(context.Context) (SecuritySettings, error) {
	return m.s, nil
}

func (m *MemSecuritySettingsStore) Save(_ context.Context, force bool) (SecuritySettings, error) {
	m.s = SecuritySettings{ForcePasswordChange: force, UpdatedAt: timeNowUTC()}
	return m.s, nil
}

// PgSecuritySettingsStore 单行表 security_settings。
type PgSecuritySettingsStore struct{ pool *pgxpool.Pool }

func NewPgSecuritySettingsStore(pool *pgxpool.Pool) *PgSecuritySettingsStore {
	return &PgSecuritySettingsStore{pool: pool}
}

func (s *PgSecuritySettingsStore) Get(ctx context.Context) (SecuritySettings, error) {
	var out SecuritySettings
	err := s.pool.QueryRow(ctx,
		`SELECT force_password_change, updated_at FROM security_settings WHERE id = 1`).
		Scan(&out.ForcePasswordChange, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return SecuritySettings{}, nil
	}
	return out, err
}

func (s *PgSecuritySettingsStore) Save(ctx context.Context, force bool) (SecuritySettings, error) {
	var out SecuritySettings
	err := s.pool.QueryRow(ctx,
		`INSERT INTO security_settings(id, force_password_change, updated_at)
		 VALUES(1, $1, now())
		 ON CONFLICT (id) DO UPDATE SET force_password_change = EXCLUDED.force_password_change, updated_at = now()
		 RETURNING force_password_change, updated_at`, force).
		Scan(&out.ForcePasswordChange, &out.UpdatedAt)
	return out, err
}

// SecuritySettingsHandler GET/PUT /system/security-settings。
type SecuritySettingsHandler struct{ store SecuritySettingsStore }

func NewSecuritySettingsHandler(store SecuritySettingsStore) *SecuritySettingsHandler {
	return &SecuritySettingsHandler{store: store}
}

func toGenSecuritySettings(s SecuritySettings) apigen.SecuritySettings {
	out := apigen.SecuritySettings{ForcePasswordChange: s.ForcePasswordChange}
	if !s.UpdatedAt.IsZero() {
		t := s.UpdatedAt
		out.UpdatedAt = &t
	}
	return out
}

func (h *SecuritySettingsHandler) GetSecuritySettings(c *gin.Context) {
	s, err := h.store.Get(c.Request.Context())
	if err != nil {
		WriteProblem(c, 500, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.JSON(200, toGenSecuritySettings(s))
}

func (h *SecuritySettingsHandler) UpdateSecuritySettings(c *gin.Context) {
	var body apigen.SecuritySettingsUpdate
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, "BAD_REQUEST", err.Error())
		return
	}
	s, err := h.store.Save(c.Request.Context(), body.ForcePasswordChange)
	if err != nil {
		WriteProblem(c, 500, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.JSON(200, toGenSecuritySettings(s))
}
