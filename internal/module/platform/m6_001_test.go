package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
)

func newPasswordRouter(t *testing.T, forceChange ...bool) (*gin.Engine, UserStore) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store := NewMemUserStore()
	if err := EnsureBootstrap(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	var sec SecuritySettingsStore
	if len(forceChange) > 0 {
		mem := NewMemSecuritySettingsStore()
		if _, err := mem.Save(context.Background(), forceChange[0]); err != nil {
			t.Fatal(err)
		}
		sec = mem
	}
	h := NewAuthHandler(store, NewTokenBlacklist(), nil, sec)
	r := gin.New()
	r.POST("/api/v1/auth/login", h.AuthLogin)
	r.POST("/api/v1/auth/password", h.ChangeAuthPassword)
	r.GET("/api/v1/auth/user/info", h.GetAuthUserInfo)
	return r, store
}

func loginWith(t *testing.T, r http.Handler, username, password string) (*httptest.ResponseRecorder, apigen.AuthLoginResult) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"username":"`+username+`","password":"`+password+`"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	var res apigen.AuthLoginResult
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	return w, res
}

func TestValidatePassword(t *testing.T) {
	cases := []struct {
		name string
		user string
		pw   string
		ok   bool
	}{
		{"too short", "alice", "Ab1!xyz", false},
		{"only lower", "alice", "abcdefghijkl", false},
		{"two classes", "alice", "abcdefgh12", false},
		{"three classes", "alice", "Abcdefgh12", true},
		{"contains username", "alice", "Alice-Pass1!", false},
		{"whitespace", "alice", "Abc defgh12!", false},
		{"common", "alice", "admin123", false},
		{"strong", "alice", "Qa-Passw0rd!23", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidatePassword(c.user, c.pw)
			if c.ok && err != nil {
				t.Fatalf("want ok, got %v", err)
			}
			if !c.ok && err == nil {
				t.Fatal("want error, got nil")
			}
		})
	}
}

func TestBootstrapGeneratesRandomPasswordAndMustChange(t *testing.T) {
	t.Setenv("IPAM_POC_PASSWORD", "")
	dir := t.TempDir()
	t.Setenv("IPAM_RUN_DIR", dir)
	store := NewMemUserStore()
	if err := EnsureBootstrap(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	rec, ok, err := store.GetByUsername(context.Background(), "admin")
	if err != nil || !ok {
		t.Fatalf("admin missing: %v", err)
	}
	if rec.MustChangePassword {
		t.Fatal("首次引导不应强制改密（由全局策略决定）")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "initial-admin-password"))
	if err != nil {
		t.Fatalf("初始口令文件: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 || lines[0] != "admin" {
		t.Fatalf("初始口令文件格式异常: %q", raw)
	}
	if len(lines[1]) < 16 || !CheckPassword(rec.PasswordHash, lines[1]) {
		t.Fatal("随机初始口令长度/散列校验失败")
	}
}

func TestForcePasswordChangeDefaultOff(t *testing.T) {
	r, _ := newPasswordRouter(t)
	w, res := loginWith(t, r, "admin", "admin123")
	if w.Code != http.StatusOK {
		t.Fatalf("login %d", w.Code)
	}
	if res.MustChangePassword {
		t.Fatal("默认策略应关闭强制改密（便于调试），登录不应要求改密")
	}
}

func TestChangePasswordFlow(t *testing.T) {
	r, store := newPasswordRouter(t, true)
	// 模拟“新建/重置后需改密”的账号：策略开启 + 用户标记
	admin, _, _ := store.GetByUsername(context.Background(), "admin")
	if _, err := store.Update(context.Background(), admin.ID, UserUpdateInput{MustChangePassword: ptrBool(true)}); err != nil {
		t.Fatal(err)
	}

	w, res := loginWith(t, r, "admin", "admin123")
	if w.Code != http.StatusOK {
		t.Fatalf("login %d: %s", w.Code, w.Body.String())
	}
	if !res.MustChangePassword {
		t.Fatal("首次登录应返回 mustChangePassword=true")
	}

	// 旧口令错误
	w = doJSON(r, http.MethodPost, "/api/v1/auth/password", res.AccessToken,
		`{"oldPassword":"wrong","newPassword":"Qa-Passw0rd!23"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("old password mismatch want 401, got %d", w.Code)
	}

	// 弱口令
	w = doJSON(r, http.MethodPost, "/api/v1/auth/password", res.AccessToken,
		`{"oldPassword":"admin123","newPassword":"12345678"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "WEAK_PASSWORD") {
		t.Fatalf("weak password want 400 WEAK_PASSWORD, got %d %s", w.Code, w.Body.String())
	}

	// 与旧口令相同
	w = doJSON(r, http.MethodPost, "/api/v1/auth/password", res.AccessToken,
		`{"oldPassword":"admin123","newPassword":"admin123"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("same password want 400, got %d", w.Code)
	}

	// 成功
	w = doJSON(r, http.MethodPost, "/api/v1/auth/password", res.AccessToken,
		`{"oldPassword":"admin123","newPassword":"Qa-Passw0rd!23"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("change password %d: %s", w.Code, w.Body.String())
	}
	var changed apigen.AuthLoginResult
	if err := json.Unmarshal(w.Body.Bytes(), &changed); err != nil {
		t.Fatal(err)
	}
	if changed.MustChangePassword || changed.AccessToken == "" || changed.RefreshToken == "" {
		t.Fatalf("改密后响应异常: %+v", changed)
	}

	rec, _, _ := store.GetByUsername(context.Background(), "admin")
	if rec.MustChangePassword {
		t.Fatal("改密后 mustChangePassword 应清除")
	}
	if !CheckPassword(rec.PasswordHash, "Qa-Passw0rd!23") {
		t.Fatal("新口令散列未生效")
	}
	if rec.TokenVersion <= 1 {
		t.Fatalf("改密后 token_version 应递增: %d", rec.TokenVersion)
	}

	// 旧口令不能登录，新口令可以
	if w, _ := loginWith(t, r, "admin", "admin123"); w.Code != http.StatusUnauthorized {
		t.Fatalf("旧口令登录应 401, got %d", w.Code)
	}
	w, res2 := loginWith(t, r, "admin", "Qa-Passw0rd!23")
	if w.Code != http.StatusOK || res2.MustChangePassword {
		t.Fatalf("新口令登录异常: %d %+v", w.Code, res2)
	}

	// 新令牌的 user/info 标记已清除
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/user/info", nil)
	req.Header.Set("Authorization", "Bearer "+changed.AccessToken)
	wi := httptest.NewRecorder()
	r.ServeHTTP(wi, req)
	if wi.Code != http.StatusOK {
		t.Fatalf("userinfo %d: %s", wi.Code, wi.Body.String())
	}
	var info apigen.UserInfo
	_ = json.Unmarshal(wi.Body.Bytes(), &info)
	if info.MustChangePassword {
		t.Fatal("user/info 的 mustChangePassword 应为 false")
	}
}

func TestSecuritySettingsControlsNewUserFlag(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewMemUserStore()
	if err := EnsureBootstrap(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	sec := NewMemSecuritySettingsStore()
	ah := NewAuthHandler(store, NewTokenBlacklist(), nil, sec)
	uh := NewUserHandler(store, sec)
	sh := NewSecuritySettingsHandler(sec)
	r := gin.New()
	r.POST("/api/v1/auth/login", ah.AuthLogin)
	r.POST("/api/v1/users", uh.CreateUser)
	r.PUT("/api/v1/system/security-settings", sh.UpdateSecuritySettings)
	r.GET("/api/v1/system/security-settings", sh.GetSecuritySettings)

	_, login := loginWith(t, r, "admin", "admin123")
	token := login.AccessToken

	// 默认关闭：新建账号不标记需改密
	if w := doJSON(r, http.MethodPost, "/api/v1/users", token,
		`{"username":"op-off","password":"Qa-Passw0rd!23","roles":["user"]}`); w.Code != http.StatusCreated {
		t.Fatalf("create off %d: %s", w.Code, w.Body.String())
	}
	off, _, _ := store.GetByUsername(context.Background(), "op-off")
	if off.MustChangePassword {
		t.Fatal("策略关闭时新建账号不应标记需改密")
	}

	// 开启后：新建账号标记需改密
	if w := doJSON(r, http.MethodPut, "/api/v1/system/security-settings", token,
		`{"forcePasswordChange":true}`); w.Code != http.StatusOK {
		t.Fatalf("enable %d: %s", w.Code, w.Body.String())
	}
	if w := doJSON(r, http.MethodPost, "/api/v1/users", token,
		`{"username":"op-on","password":"Qa-Passw0rd!23","roles":["user"]}`); w.Code != http.StatusCreated {
		t.Fatalf("create on %d: %s", w.Code, w.Body.String())
	}
	on, _, _ := store.GetByUsername(context.Background(), "op-on")
	if !on.MustChangePassword {
		t.Fatal("策略开启时新建账号应标记需改密")
	}
	// 该账号登录返回 mustChangePassword=true
	w, res := loginWith(t, r, "op-on", "Qa-Passw0rd!23")
	if w.Code != http.StatusOK || !res.MustChangePassword {
		t.Fatalf("策略开启时登录应要求改密: %d %+v", w.Code, res)
	}
}
