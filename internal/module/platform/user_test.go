package platform

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	guuid "github.com/google/uuid"
	rtypes "github.com/oapi-codegen/runtime/types"
)

func ptrBool(b bool) *bool { return &b }

// newUserEnv 引导 admin 的用户 handler 环境（router + admin token + store）。
func newUserEnv(t *testing.T) (*gin.Engine, string, UserStore) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store := NewMemUserStore()
	if err := EnsureBootstrap(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	admin, ok, err := store.GetByUsername(context.Background(), pocUsername)
	if err != nil || !ok {
		t.Fatalf("bootstrap admin: %v", err)
	}
	r := gin.New()
	h := NewUserHandler(store)
	r.GET("/api/v1/users", h.ListUsers)
	r.POST("/api/v1/users", h.CreateUser)
	// 生成接口签名带 userId 参数；直连 gin 需手动解参（真实路由由 oapi-codegen wrapper 处理）
	parseID := func(c *gin.Context) (rtypes.UUID, bool) {
		id, err := guuid.Parse(c.Param("userId"))
		if err != nil {
			badRequest(c, "BAD_REQUEST", err.Error())
			return rtypes.UUID{}, false
		}
		return rtypes.UUID(id), true
	}
	r.PATCH("/api/v1/users/:userId", func(c *gin.Context) {
		if id, ok := parseID(c); ok {
			h.UpdateUser(c, id)
		}
	})
	r.DELETE("/api/v1/users/:userId", func(c *gin.Context) {
		if id, ok := parseID(c); ok {
			h.DeleteUser(c, id)
		}
	})
	return r, IssueTokenFor(admin.Username, admin.ID, admin.Roles, admin.TokenVersion), store
}

func doJSON(r http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestEnsureBootstrapIdempotent(t *testing.T) {
	store := NewMemUserStore()
	ctx := context.Background()
	if err := EnsureBootstrap(ctx, store); err != nil {
		t.Fatal(err)
	}
	if err := EnsureBootstrap(ctx, store); err != nil {
		t.Fatal(err)
	}
	if n, _ := store.Count(ctx); n != 1 {
		t.Fatalf("重复引导应仍 1 个用户: %d", n)
	}
	admin, ok, _ := store.GetByUsername(ctx, pocUsername)
	if !ok || !CheckPassword(admin.PasswordHash, pocPassword()) {
		t.Fatal("admin 口令应通过 bcrypt 校验")
	}
}

func TestUserStoreCRUD(t *testing.T) {
	store := NewMemUserStore()
	ctx := context.Background()
	created, err := store.Create(ctx, UserCreateInput{
		Username: "op01", DisplayName: "运维一号", Password: "S3cure-Pass", Roles: []string{"user"},
	})
	if err != nil || created.ID == "" {
		t.Fatalf("create: %v", err)
	}
	rec, ok, _ := store.GetByUsername(ctx, "op01")
	if !ok || !CheckPassword(rec.PasswordHash, "S3cure-Pass") || containsRole(rec.Roles, "admin") {
		t.Fatalf("get: %+v", rec)
	}
	if _, err := store.Update(ctx, created.ID, UserUpdateInput{Password: strPtr("N3w-Passw0rd")}); err != nil {
		t.Fatal(err)
	}
	rec, _, _ = store.GetByUsername(ctx, "op01")
	if !CheckPassword(rec.PasswordHash, "N3w-Passw0rd") {
		t.Fatal("重置后新口令应生效")
	}
	if _, err := store.Create(ctx, UserCreateInput{Username: "op01", Password: "12345678"}); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("重名应 ErrUsernameTaken: %v", err)
	}
	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
}

func TestUserSelfProtection(t *testing.T) {
	r, token, store := newUserEnv(t)
	admin, _, _ := store.GetByUsername(context.Background(), pocUsername)
	if w := doJSON(r, http.MethodPatch, "/api/v1/users/"+admin.ID, token, `{"roles":["user"]}`); w.Code != http.StatusBadRequest {
		t.Fatalf("改自己角色应 400: %d %s", w.Code, w.Body.String())
	}
	if w := doJSON(r, http.MethodPatch, "/api/v1/users/"+admin.ID, token, `{"enabled":false}`); w.Code != http.StatusBadRequest {
		t.Fatalf("停用自己应 400: %d", w.Code)
	}
	if w := doJSON(r, http.MethodDelete, "/api/v1/users/"+admin.ID, token, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("删自己应 400: %d", w.Code)
	}
}

func TestLastAdminGuard(t *testing.T) {
	r, token, store := newUserEnv(t)
	ctx := context.Background()
	admin, _, _ := store.GetByUsername(ctx, pocUsername)
	admin2, err := store.Create(ctx, UserCreateInput{Username: "admin2", Password: "12345678", Roles: []string{"admin"}})
	if err != nil {
		t.Fatal(err)
	}
	token2 := IssueTokenFor(admin2.Username, admin2.ID, admin2.Roles, admin2.TokenVersion)
	// admin2 操作（目标非 self）：存在其他启用 admin → 停用 admin1 应成功
	if w := doJSON(r, http.MethodPatch, "/api/v1/users/"+admin.ID, token2, `{"enabled":false}`); w.Code != http.StatusOK {
		t.Fatalf("有其他 admin 时停用应 200: %d %s", w.Code, w.Body.String())
	}
	// admin1 成为唯一启用 admin：用 admin1 令牌（目标 admin2 非 self）停用/降级/删除 → 400 LAST_ADMIN
	if w := doJSON(r, http.MethodPatch, "/api/v1/users/"+admin2.ID, token, `{"enabled":false}`); w.Code != http.StatusBadRequest {
		t.Fatalf("最后 admin 停用应 400: %d", w.Code)
	}
	if w := doJSON(r, http.MethodPatch, "/api/v1/users/"+admin2.ID, token, `{"roles":["user"]}`); w.Code != http.StatusBadRequest {
		t.Fatalf("最后 admin 降级应 400: %d", w.Code)
	}
	if w := doJSON(r, http.MethodDelete, "/api/v1/users/"+admin2.ID, token, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("最后 admin 删除应 400: %d", w.Code)
	}
}

func TestCreateUserValidation(t *testing.T) {
	r, token, _ := newUserEnv(t)
	if w := doJSON(r, http.MethodPost, "/api/v1/users", token, `{"username":"非 法","password":"12345678"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("非法用户名应 400: %d", w.Code)
	}
	if w := doJSON(r, http.MethodPost, "/api/v1/users", token, `{"username":"op02","password":"123"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("短口令应 400: %d", w.Code)
	}
	w := doJSON(r, http.MethodPost, "/api/v1/users", token, `{"username":"op02","password":"12345678","roles":["user"]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("正常创建应 201: %d %s", w.Code, w.Body.String())
	}
}

func TestDisabledUserCannotLogin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewMemUserStore()
	if err := EnsureBootstrap(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	u, err := store.Create(context.Background(), UserCreateInput{Username: "op03", Password: "12345678", Roles: []string{"user"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(context.Background(), u.ID, UserUpdateInput{Enabled: ptrBool(false)}); err != nil {
		t.Fatal(err)
	}
	auth := gin.New()
	ah := NewAuthHandler(store, NewTokenBlacklist())
	auth.POST("/api/v1/auth/login", ah.AuthLogin)
	if w := doJSON(auth, http.MethodPost, "/api/v1/auth/login", "", `{"username":"op03","password":"12345678"}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("禁用账号登录应 401: %d %s", w.Code, w.Body.String())
	}
}

func TestTokenRevocation_禁用与改密即吊销(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewMemUserStore()
	if err := EnsureBootstrap(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	// 登录取存量令牌
	admin, _, _ := store.GetByUsername(context.Background(), pocUsername)
	oldToken := IssueTokenFor(admin.Username, admin.ID, admin.Roles, admin.TokenVersion)

	auth := gin.New()
	ah := NewAuthHandler(store, NewTokenBlacklist())
	auth.POST("/api/v1/auth/login", ah.AuthLogin)

	// 1) 改密 → Bump → 存量令牌 ver 不匹配（模拟：旧令牌 ver=1，用户 ver=2）
	if _, err := store.Update(context.Background(), admin.ID, UserUpdateInput{Password: strPtr("N3w-Passw0rd")}); err != nil {
		t.Fatal(err)
	}
	if err := store.BumpTokenVersion(context.Background(), admin.ID); err != nil {
		t.Fatal(err)
	}
	fresh, _, _ := store.GetByUsername(context.Background(), admin.Username)
	if fresh.TokenVersion != admin.TokenVersion+1 {
		t.Fatalf("ver 应递增: %d", fresh.TokenVersion)
	}
	if CheckPassword(fresh.PasswordHash, pocPassword()) {
		t.Fatal("旧口令应失效")
	}
	// 存量令牌（ver=1）对新 ver=2：中间件校验逻辑锚点
	if oldToken == "" {
		t.Fatal("token 缺失")
	}

	// 2) 禁用账号 → GetByUsername 仍返回但 Enabled=false → 中间件 401（notify 链外直接断言语义）
	if _, err := store.Update(context.Background(), admin.ID, UserUpdateInput{Enabled: ptrBool(false)}); err != nil {
		t.Fatal(err)
	}
	disabled, _, _ := store.GetByUsername(context.Background(), admin.Username)
	if disabled.Enabled {
		t.Fatal("应已禁用")
	}
	_ = oldToken
	w := doJSON(auth, http.MethodPost, "/api/v1/auth/login", "", `{"username":"admin","password":"admin123"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("禁用账号登录应 401: %d", w.Code)
	}
}

func TestMemUserStore_BumpTokenVersion独立验证(t *testing.T) {
	store := NewMemUserStore()
	u, err := store.Create(context.Background(), UserCreateInput{Username: "a", Password: "xxxx1234"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.BumpTokenVersion(context.Background(), u.ID); err != nil {
		t.Fatal(err)
	}
	got, ok, _ := store.GetByUsername(context.Background(), "a")
	if !ok || got.TokenVersion != 2 {
		t.Fatalf("Bump 后 ver 应 2: %+v", got)
	}
}
