package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
)

func refreshRouter(t *testing.T) (*gin.Engine, UserStore, *TokenBlacklist) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store := NewMemUserStore()
	if err := EnsureBootstrap(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	bl := NewTokenBlacklist()
	h := NewAuthHandler(store, bl, nil)
	r := gin.New()
	r.POST("/api/v1/auth/login", h.AuthLogin)
	r.POST("/api/v1/auth/refresh", h.AuthRefresh)
	r.POST("/api/v1/auth/logout", h.LogoutAuth)
	return r, store, bl
}

// loginPair 登录并断言响应同时携带 accessToken/refreshToken（M5-032 契约）。
func loginPair(t *testing.T, r http.Handler) apigen.AuthLoginResult {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"username":"admin","password":"admin123"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("login %d: %s", w.Code, w.Body.String())
	}
	var res apigen.AuthLoginResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.AccessToken == "" || res.RefreshToken == "" {
		t.Fatalf("登录响应须同时携带 accessToken/refreshToken: %s", w.Body.String())
	}
	return res
}

func postRefresh(t *testing.T, r http.Handler, body, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	r.ServeHTTP(w, req)
	return w
}

// Given 已登录令牌对，When 以 refreshToken 刷新，Then 颁发新令牌对、旧刷新 jti 入黑名单、重放被拒。
func TestAuthRefresh_轮换吊销旧刷新令牌(t *testing.T) {
	r, _, bl := refreshRouter(t)
	pair := loginPair(t, r)
	oldClaims, err := ParseJWT(pair.RefreshToken)
	if err != nil {
		t.Fatalf("parse refresh: %v", err)
	}
	if oldClaims.Kind != tokenKindRefresh || oldClaims.JTI == "" {
		t.Fatalf("刷新令牌应携带 kind=refresh 与 jti: %+v", oldClaims)
	}
	w := postRefresh(t, r, `{"refreshToken":"`+pair.RefreshToken+`"}`, "")
	if w.Code != 200 {
		t.Fatalf("refresh %d: %s", w.Code, w.Body.String())
	}
	var res apigen.AuthRefreshResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.AccessToken == "" || res.RefreshToken == "" {
		t.Fatalf("响应须为新令牌对: %s", w.Body.String())
	}
	if res.RefreshToken == pair.RefreshToken || res.AccessToken == pair.AccessToken {
		t.Fatal("轮换后令牌应与旧令牌互异")
	}
	if !bl.Revoked(oldClaims.JTI) {
		t.Fatal("旧刷新令牌 jti 应已入黑名单（先吊销后签发）")
	}
	if _, err := ParseJWT(res.AccessToken); err != nil {
		t.Fatalf("新访问令牌应可用: %v", err)
	}
	if w2 := postRefresh(t, r, `{"refreshToken":"`+pair.RefreshToken+`"}`, ""); w2.Code != http.StatusUnauthorized {
		t.Fatalf("重放旧刷新令牌应 401: %d", w2.Code)
	}
}

// Given 访问令牌，When 作为 refreshToken 提交，Then 401（kind 校验拒绝串用）。
func TestAuthRefresh_访问令牌不得充当刷新令牌(t *testing.T) {
	r, _, _ := refreshRouter(t)
	pair := loginPair(t, r)
	if w := postRefresh(t, r, `{"refreshToken":"`+pair.AccessToken+`"}`, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("访问令牌充当刷新令牌应 401: %d", w.Code)
	}
}

// Given 过期 1h 的访问令牌（7d 宽限内），When 经 Authorization 头降级刷新，Then 颁发新对。
func TestAuthRefresh_降级路径过期访问令牌可刷新(t *testing.T) {
	r, store, _ := refreshRouter(t)
	rec, _, _ := store.GetByUsername(context.Background(), "admin")
	expired := IssueJWT(JWTClaims{
		Sub: rec.Username, UID: rec.ID, Roles: rec.Roles, Typ: "user", Ver: rec.TokenVersion,
		Exp: time.Now().Add(-time.Hour).Unix(),
	}, time.Now())
	w := postRefresh(t, r, `{}`, expired)
	if w.Code != 200 {
		t.Fatalf("宽限窗口内过期访问令牌应可刷新: %d %s", w.Code, w.Body.String())
	}
	var res apigen.AuthRefreshResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.AccessToken == "" || res.RefreshToken == "" {
		t.Fatal("降级路径也应返回新令牌对")
	}
}

// Given 过期 8 天的访问令牌（超出宽限窗口），When 降级刷新，Then 401。
func TestAuthRefresh_降级路径超出宽限窗口(t *testing.T) {
	r, store, _ := refreshRouter(t)
	rec, _, _ := store.GetByUsername(context.Background(), "admin")
	ancient := IssueJWT(JWTClaims{
		Sub: rec.Username, UID: rec.ID, Roles: rec.Roles, Typ: "user", Ver: rec.TokenVersion,
		Exp: time.Now().Add(-8 * 24 * time.Hour).Unix(),
	}, time.Now())
	if w := postRefresh(t, r, `{}`, ancient); w.Code != http.StatusUnauthorized {
		t.Fatalf("超出宽限窗口应 401: %d", w.Code)
	}
}

// Given 已登出的访问令牌，When 降级刷新，Then 401（黑名单拦截）。
func TestAuthRefresh_降级路径已吊销令牌(t *testing.T) {
	r, _, _ := refreshRouter(t)
	pair := loginPair(t, r)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("logout %d", w.Code)
	}
	if w2 := postRefresh(t, r, `{}`, pair.AccessToken); w2.Code != http.StatusUnauthorized {
		t.Fatalf("已吊销访问令牌降级刷新应 401: %d", w2.Code)
	}
}

// Given 已登录令牌对，When 登出且请求体携带 refreshToken，Then 访问/刷新双 jti 均入黑名单。
func TestLogout_双令牌吊销(t *testing.T) {
	r, _, bl := refreshRouter(t)
	pair := loginPair(t, r)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout",
		strings.NewReader(`{"refreshToken":"`+pair.RefreshToken+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("logout %d", w.Code)
	}
	ac, _ := ParseJWT(pair.AccessToken)
	rc, _ := ParseJWT(pair.RefreshToken)
	if !bl.Revoked(ac.JTI) || !bl.Revoked(rc.JTI) {
		t.Fatalf("访问/刷新 jti 应同时吊销: access=%v refresh=%v", bl.Revoked(ac.JTI), bl.Revoked(rc.JTI))
	}
}

// Given 同一客户端 IP，When 1 分钟内第 6 次登录，Then 429 且带 Retry-After；时钟越过窗口后恢复。
func TestLogin_限流429与冷却恢复(t *testing.T) {
	r, _, _ := refreshRouter(t)
	orig := timeNowUTC
	cur := time.Now()
	timeNowUTC = func() time.Time { return cur }
	t.Cleanup(func() { timeNowUTC = orig })

	attempt := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
			strings.NewReader(`{"username":"admin","password":"admin123"}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}
	for i := 0; i < 5; i++ {
		if w := attempt(); w.Code == http.StatusTooManyRequests {
			t.Fatalf("第 %d 次登录不应被限流", i+1)
		}
	}
	w := attempt()
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("第 6 次登录应 429: %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("429 响应应携带 Retry-After")
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("限流错误应为 problem+json: %s", ct)
	}
	cur = cur.Add(61 * time.Second)
	if w := attempt(); w.Code != 200 {
		t.Fatalf("冷却窗口过后合法重试应成功: %d", w.Code)
	}
}

// Given 刷新令牌，When 经 RBAC 中间件访问业务接口，Then 401（7d 令牌不得获得业务访问权）。
func TestRBAC_刷新令牌不得访问业务(t *testing.T) {
	r, store, _ := rbacRouter(t)
	rec, _, _ := store.GetByUsername(context.Background(), "admin")
	rt := IssueRefreshTokenFor(rec.Username, rec.ID, rec.TokenVersion)
	if w := do(r, http.MethodGet, "/api/v1/dashboard", rt); w.Code != http.StatusUnauthorized {
		t.Fatalf("刷新令牌访问业务接口应 401: %d", w.Code)
	}
}
