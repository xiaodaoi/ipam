package platform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func tokenFor(sub, typ string, roles ...string) string {
	return IssueJWT(JWTClaims{Sub: sub, UID: "9", Roles: roles, Typ: typ}, time.Now())
}

func rbacRouter(t *testing.T) (*gin.Engine, UserStore, *TokenBlacklist) {
	gin.SetMode(gin.TestMode)
	store := NewMemUserStore()
	bl := NewTokenBlacklist()
	// M5-010：中间件查库校验 enabled/ver——预置与测试令牌 Sub 匹配的用户
	for _, u := range []UserCreateInput{
		{Username: "admin", Password: "x", Roles: []string{"admin"}},
		{Username: "guest", Password: "x", Roles: []string{"user"}},
		{Username: "ci-bot", Password: "x", Roles: []string{"admin"}}, // bot 令牌语义（Typ=bot）的用户主体
	} {
		if _, err := store.Create(context.Background(), u); err != nil {
			t.Fatal(err)
		}
	}
	r := gin.New()
	r.Use(NewRBACMiddleware(store, bl, nil))
	r.POST("/api/v1/auth/login", func(c *gin.Context) { c.Status(200) })
	r.POST("/api/v1/orgs", func(c *gin.Context) { c.Status(201) })
	r.DELETE("/api/v1/upstreams/:id", func(c *gin.Context) { c.Status(204) })
	r.GET("/api/v1/dashboard", func(c *gin.Context) { c.Status(200) })
	authH := NewAuthHandler(store, bl, nil)
	r.POST("/api/v1/auth/logout", authH.LogoutAuth)
	return r, store, bl
}

func TestRBAC_登出后令牌立即401(t *testing.T) {
	r, store, bl := rbacRouter(t)
	admin, _, _ := store.GetByUsername(context.Background(), "admin")
	tok := IssueTokenFor(admin.Username, admin.ID, admin.Roles, admin.TokenVersion)
	if w := do(r, http.MethodPost, "/api/v1/orgs", tok); w.Code != 201 {
		t.Fatalf("登出前 POST 应 201: %d", w.Code)
	}
	// M5-011：登出 → 黑名单 → 同令牌立即 401
	if w := do(r, http.MethodPost, "/api/v1/auth/logout", tok); w.Code != 200 {
		t.Fatalf("logout 应 200: %d", w.Code)
	}
	if !bl.Revoked(TokenHash(tok)) {
		t.Fatal("黑名单未命中")
	}
	if w := do(r, http.MethodGet, "/api/v1/dashboard", tok); w.Code != http.StatusUnauthorized {
		t.Fatalf("登出后同令牌应 401: %d", w.Code)
	}
	// 其他用户的令牌不受影响（登出只吊销被登出的令牌本身；
	// 同参数签发会产生同一 JWT 字符串，故用不同用户的令牌验证隔离性）
	guestTok := tokenFor("guest", "user", "user")
	if w := do(r, http.MethodGet, "/api/v1/dashboard", guestTok); w.Code != 200 {
		t.Fatalf("guest 令牌应 200: %d", w.Code)
	}
}

func do(r *gin.Engine, method, path, token string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	r.ServeHTTP(w, req)
	return w
}

func TestRBACAdminWriteAllowed(t *testing.T) {
	r, _, _ := rbacRouter(t)
	tok := tokenFor("admin", "user", "admin")
	if w := do(r, http.MethodPost, "/api/v1/orgs", tok); w.Code != 201 {
		t.Fatalf("admin POST want 201, got %d", w.Code)
	}
	if w := do(r, http.MethodDelete, "/api/v1/upstreams/x", tok); w.Code != 204 {
		t.Fatalf("admin DELETE want 204, got %d", w.Code)
	}
}

func TestRBACUserWriteForbidden(t *testing.T) {
	r, _, _ := rbacRouter(t)
	tok := tokenFor("guest", "user", "user")
	w := do(r, http.MethodPost, "/api/v1/orgs", tok)
	if w.Code != http.StatusForbidden {
		t.Fatalf("user POST want 403, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "FORBIDDEN") {
		t.Fatalf("body missing FORBIDDEN: %s", w.Body.String())
	}
	// 读端点不受限
	if w := do(r, http.MethodGet, "/api/v1/dashboard", tok); w.Code != 200 {
		t.Fatalf("user GET want 200, got %d", w.Code)
	}
}

func TestRBACNoToken401(t *testing.T) {
	r, _, _ := rbacRouter(t)
	if w := do(r, http.MethodPost, "/api/v1/orgs", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("no-token want 401, got %d", w.Code)
	}
}

func TestRBACLoginPathBypass(t *testing.T) {
	r, _, _ := rbacRouter(t)
	if w := do(r, http.MethodPost, "/api/v1/auth/login", ""); w.Code != 200 {
		t.Fatalf("login want 200 bypass, got %d", w.Code)
	}
}

func TestRBACBotAdminAllowed(t *testing.T) {
	r, _, _ := rbacRouter(t)
	tok := tokenFor("ci-bot", "bot", "admin")
	if w := do(r, http.MethodPost, "/api/v1/orgs", tok); w.Code != 201 {
		t.Fatalf("bot-admin POST want 201, got %d", w.Code)
	}
}

func TestRBAC_停用账号存量令牌立即401(t *testing.T) {
	r, store, _ := rbacRouter(t)
	tok := tokenFor("admin", "user", "admin") // Ver=0（兼容语义）
	if w := do(r, http.MethodPost, "/api/v1/orgs", tok); w.Code != 201 {
		t.Fatalf("停用前 POST 应 201: %d", w.Code)
	}
	admin, _, _ := store.GetByUsername(context.Background(), "admin")
	if _, err := store.Update(context.Background(), admin.ID, UserUpdateInput{Enabled: ptrBool(false)}); err != nil {
		t.Fatal(err)
	}
	// M5-010：停用后存量令牌（enabled=false）→ 401 TOKEN_REVOKED
	if w := do(r, http.MethodPost, "/api/v1/orgs", tok); w.Code != http.StatusUnauthorized {
		t.Fatalf("停用后存量令牌应 401: %d", w.Code)
	}
}

func TestRBAC_改密后旧会话版本令牌401(t *testing.T) {
	r, store, _ := rbacRouter(t)
	admin, _, _ := store.GetByUsername(context.Background(), "admin")
	tok := IssueTokenFor(admin.Username, admin.ID, admin.Roles, admin.TokenVersion) // ver=1
	if w := do(r, http.MethodPost, "/api/v1/orgs", tok); w.Code != 201 {
		t.Fatalf("Bump 前 POST 应 201: %d", w.Code)
	}
	// M5-010：改密 → Bump → 存量令牌 ver=1 vs 用户 ver=2 → 401
	if err := store.BumpTokenVersion(context.Background(), admin.ID); err != nil {
		t.Fatal(err)
	}
	if bumped, _, _ := store.GetByUsername(context.Background(), "admin"); bumped.TokenVersion != 2 {
		t.Fatalf("Bump 未生效: ver=%d", bumped.TokenVersion)
	}
	if w := do(r, http.MethodPost, "/api/v1/orgs", tok); w.Code != http.StatusUnauthorized {
		t.Fatalf("Bump 后旧令牌应 401: %d", w.Code)
	}
}
