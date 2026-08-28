package platform

import (
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

func rbacRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(NewRBACMiddleware())
	r.POST("/api/v1/auth/login", func(c *gin.Context) { c.Status(200) })
	r.POST("/api/v1/orgs", func(c *gin.Context) { c.Status(201) })
	r.DELETE("/api/v1/upstreams/:id", func(c *gin.Context) { c.Status(204) })
	r.GET("/api/v1/dashboard", func(c *gin.Context) { c.Status(200) })
	return r
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
	r := rbacRouter()
	tok := tokenFor("admin", "user", "admin")
	if w := do(r, http.MethodPost, "/api/v1/orgs", tok); w.Code != 201 {
		t.Fatalf("admin POST want 201, got %d", w.Code)
	}
	if w := do(r, http.MethodDelete, "/api/v1/upstreams/x", tok); w.Code != 204 {
		t.Fatalf("admin DELETE want 204, got %d", w.Code)
	}
}

func TestRBACUserWriteForbidden(t *testing.T) {
	r := rbacRouter()
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
	r := rbacRouter()
	if w := do(r, http.MethodPost, "/api/v1/orgs", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("no-token want 401, got %d", w.Code)
	}
}

func TestRBACLoginPathBypass(t *testing.T) {
	r := rbacRouter()
	if w := do(r, http.MethodPost, "/api/v1/auth/login", ""); w.Code != 200 {
		t.Fatalf("login want 200 bypass, got %d", w.Code)
	}
}

func TestRBACBotAdminAllowed(t *testing.T) {
	r := rbacRouter()
	tok := tokenFor("ci-bot", "bot", "admin")
	if w := do(r, http.MethodPost, "/api/v1/orgs", tok); w.Code != 201 {
		t.Fatalf("bot-admin POST want 201, got %d", w.Code)
	}
}
