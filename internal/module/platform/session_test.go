package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	guuid "github.com/google/uuid"
	rtypes "github.com/oapi-codegen/runtime/types"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
)

// sessionEnv 构建带 RBAC 的会话测试环境（登录/列表/下线/刷新/登出）。
func sessionEnv(t *testing.T) (*gin.Engine, *SessionService, UserStore, *TokenBlacklist) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store := NewMemUserStore()
	if err := EnsureBootstrap(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	bl := NewTokenBlacklist()
	svc := NewSessionService(NewMemSessionStore(), bl)
	authH := NewAuthHandler(store, bl, nil)
	authH.SetSessions(svc)
	sessH := NewSessionHandler(svc)
	userH := NewUserHandler(store)
	userH.SetSessions(svc)

	r := gin.New()
	r.Use(NewRBACMiddleware(store, bl, nil, svc))
	r.POST("/api/v1/auth/login", authH.AuthLogin)
	r.POST("/api/v1/auth/refresh", authH.AuthRefresh)
	r.POST("/api/v1/auth/logout", authH.LogoutAuth)
	r.GET("/api/v1/auth/sessions", sessH.ListAuthSessions)
	r.DELETE("/api/v1/auth/sessions/:sessionId", func(c *gin.Context) {
		sessH.RevokeAuthSession(c, c.Param("sessionId"))
	})
	r.GET("/api/v1/system/sessions", func(c *gin.Context) {
		var params apigen.ListSystemSessionsParams
		if v := c.Query("userId"); v != "" {
			id, err := guuid.Parse(v)
			if err != nil {
				badRequest(c, "BAD_REQUEST", err.Error())
				return
			}
			uid := rtypes.UUID(id)
			params.UserId = &uid
		}
		sessH.ListSystemSessions(c, params)
	})
	r.DELETE("/api/v1/system/sessions/:sessionId", func(c *gin.Context) {
		sessH.RevokeSystemSession(c, c.Param("sessionId"))
	})
	r.PATCH("/api/v1/users/:userId", func(c *gin.Context) {
		id, err := guuid.Parse(c.Param("userId"))
		if err != nil {
			badRequest(c, "BAD_REQUEST", err.Error())
			return
		}
		userH.UpdateUser(c, rtypes.UUID(id))
	})
	r.GET("/api/v1/dashboard", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r, svc, store, bl
}

func loginAs(t *testing.T, r http.Handler) apigen.AuthLoginResult {
	t.Helper()
	w := doJSON(r, http.MethodPost, "/api/v1/auth/login", "",
		`{"username":"admin","password":"admin123"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("login %d: %s", w.Code, w.Body.String())
	}
	var res apigen.AuthLoginResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	return res
}

func listSessions(t *testing.T, r http.Handler, token string) apigen.SessionList {
	t.Helper()
	w := doJSON(r, http.MethodGet, "/api/v1/auth/sessions", token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("sessions %d: %s", w.Code, w.Body.String())
	}
	var out apigen.SessionList
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSession_登录登记与列表(t *testing.T) {
	r, _, _, _ := sessionEnv(t)
	pair := loginAs(t, r)
	claims, err := ParseJWT(pair.AccessToken)
	if err != nil || claims.SID == "" {
		t.Fatalf("访问令牌应携带 sid: %+v err=%v", claims, err)
	}
	list := listSessions(t, r, pair.AccessToken)
	if len(list.Items) != 1 || !list.Items[0].Current {
		t.Fatalf("应仅当前会话且 current=true: %+v", list.Items)
	}
	if list.Items[0].Ip == "" || list.Items[0].UserAgent == "" {
		t.Logf("httptest 未设置 UA/IP 时允许为空: %+v", list.Items[0])
	}
	if w := doJSON(r, http.MethodGet, "/api/v1/dashboard", pair.AccessToken, ""); w.Code != http.StatusOK {
		t.Fatalf("会话令牌应可访问业务: %d", w.Code)
	}
}

func TestSession_单会话强制下线(t *testing.T) {
	r, _, _, _ := sessionEnv(t)
	p1 := loginAs(t, r)
	p2 := loginAs(t, r)
	c2, _ := ParseJWT(p2.AccessToken)
	list := listSessions(t, r, p1.AccessToken)
	if len(list.Items) != 2 {
		t.Fatalf("应有 2 个会话: %+v", list.Items)
	}
	var other string
	for _, it := range list.Items {
		if it.Id == c2.SID {
			other = it.Id
		}
	}
	if other == "" {
		t.Fatal("未找到第二会话")
	}
	if w := doJSON(r, http.MethodDelete, "/api/v1/auth/sessions/"+other, p1.AccessToken, ""); w.Code != http.StatusNoContent {
		t.Fatalf("强制下线应 204: %d %s", w.Code, w.Body.String())
	}
	if w := doJSON(r, http.MethodGet, "/api/v1/dashboard", p2.AccessToken, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("被下线令牌应立即 401: %d", w.Code)
	}
	// 刷新令牌同样失效
	if w := doJSON(r, http.MethodPost, "/api/v1/auth/refresh", "", `{"refreshToken":"`+p2.RefreshToken+`"}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("被下线会话的刷新令牌应 401: %d", w.Code)
	}
	if got := listSessions(t, r, p1.AccessToken); len(got.Items) != 1 {
		t.Fatalf("下线后应仅剩当前会话: %+v", got.Items)
	}
}

func TestSession_不可下线他人会话(t *testing.T) {
	r, svc, store, _ := sessionEnv(t)
	p1 := loginAs(t, r)
	// 新建普通用户并为其登记独立会话（直接插入会话记录，避免依赖登录口令）
	u, err := store.Create(context.Background(), UserCreateInput{
		Username: "guest", Password: "Guest-Passw0rd", Roles: []string{"user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	rec, _, _ := store.GetByID(context.Background(), u.ID)
	sess := svc.NewSession(rec, "10.0.0.9", "test", time.Now().Add(time.Hour))
	svc.Create(context.Background(), sess)

	if w := doJSON(r, http.MethodDelete, "/api/v1/auth/sessions/"+sess.ID, p1.AccessToken, ""); w.Code != http.StatusNotFound {
		t.Fatalf("下线他人会话应 404: %d", w.Code)
	}
}

func TestSession_刷新沿用会话并推进过期(t *testing.T) {
	r, svc, _, _ := sessionEnv(t)
	pair := loginAs(t, r)
	oldClaims, _ := ParseJWT(pair.AccessToken)
	w := doJSON(r, http.MethodPost, "/api/v1/auth/refresh", "", `{"refreshToken":"`+pair.RefreshToken+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("refresh %d: %s", w.Code, w.Body.String())
	}
	var res apigen.AuthRefreshResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	newClaims, _ := ParseJWT(res.AccessToken)
	if newClaims.SID != oldClaims.SID || newClaims.SID == "" {
		t.Fatalf("刷新应沿用会话: old=%q new=%q", oldClaims.SID, newClaims.SID)
	}
	sess, ok := svc.Get(context.Background(), newClaims.SID)
	if !ok || sess.RevokedAt != nil {
		t.Fatalf("会话应仍有效: %+v", sess)
	}
	if !sess.ExpiresAt.After(time.Now().Add(6 * 24 * time.Hour)) {
		t.Fatalf("刷新后会话过期时间应推进: %v", sess.ExpiresAt)
	}
}

func TestSession_登出仅注销当前会话(t *testing.T) {
	r, _, _, _ := sessionEnv(t)
	p1 := loginAs(t, r)
	p2 := loginAs(t, r)
	if w := doJSON(r, http.MethodPost, "/api/v1/auth/logout", p1.AccessToken, ""); w.Code != http.StatusOK {
		t.Fatalf("logout %d", w.Code)
	}
	if w := doJSON(r, http.MethodGet, "/api/v1/dashboard", p1.AccessToken, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("登出令牌应 401: %d", w.Code)
	}
	if w := doJSON(r, http.MethodGet, "/api/v1/dashboard", p2.AccessToken, ""); w.Code != http.StatusOK {
		t.Fatalf("其他会话不受影响: %d", w.Code)
	}
	if got := listSessions(t, r, p2.AccessToken); len(got.Items) != 1 {
		t.Fatalf("登出后应仅剩另一会话: %+v", got.Items)
	}
}

func TestSession_管理端列表与强制下线(t *testing.T) {
	r, _, store, _ := sessionEnv(t)
	loginAs(t, r)
	admin, _, _ := store.GetByUsername(context.Background(), "admin")
	adminTok := IssueTokenFor(admin.Username, admin.ID, admin.Roles, admin.TokenVersion)

	w := doJSON(r, http.MethodGet, "/api/v1/system/sessions", adminTok, "")
	if w.Code != http.StatusOK {
		t.Fatalf("system sessions %d: %s", w.Code, w.Body.String())
	}
	var list apigen.SessionList
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) == 0 {
		t.Fatal("管理端应可见活跃会话")
	}
	target := list.Items[0]
	if w := doJSON(r, http.MethodDelete, "/api/v1/system/sessions/"+target.Id, adminTok, ""); w.Code != http.StatusNoContent {
		t.Fatalf("管理端下线应 204: %d %s", w.Code, w.Body.String())
	}
	if got := listSessions(t, r, adminTok); len(got.Items) != 0 {
		t.Fatalf("目标会话应已移除: %+v", got.Items)
	}
}

func TestSession_账号角色变更吊销全部会话(t *testing.T) {
	r, _, store, _ := sessionEnv(t)
	op, err := store.Create(context.Background(), UserCreateInput{
		Username: "op01", Password: "Operator-Passw0rd", Roles: []string{"user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	w := doJSON(r, http.MethodPost, "/api/v1/auth/login", "",
		`{"username":"op01","password":"Operator-Passw0rd"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("op login %d: %s", w.Code, w.Body.String())
	}
	var opPair apigen.AuthLoginResult
	if err := json.Unmarshal(w.Body.Bytes(), &opPair); err != nil {
		t.Fatal(err)
	}
	admin, _, _ := store.GetByUsername(context.Background(), "admin")
	adminTok := IssueTokenFor(admin.Username, admin.ID, admin.Roles, admin.TokenVersion)
	body := `{"roles":["admin"]}`
	if w := doJSON(r, http.MethodPatch, "/api/v1/users/"+op.ID, adminTok, body); w.Code != http.StatusOK {
		t.Fatalf("update %d: %s", w.Code, w.Body.String())
	}
	if w := doJSON(r, http.MethodGet, "/api/v1/dashboard", opPair.AccessToken, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("角色变更后旧会话应 401: %d", w.Code)
	}
	rec, _, _ := store.GetByID(context.Background(), op.ID)
	if rec.TokenVersion <= 1 {
		t.Fatalf("角色变更应提升 token_version: %d", rec.TokenVersion)
	}
}

func TestSession_仅改显示名不吊销会话(t *testing.T) {
	r, _, store, _ := sessionEnv(t)
	op, err := store.Create(context.Background(), UserCreateInput{
		Username: "op02", Password: "Operator-Passw0rd", Roles: []string{"user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	w := doJSON(r, http.MethodPost, "/api/v1/auth/login", "",
		`{"username":"op02","password":"Operator-Passw0rd"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("op login %d: %s", w.Code, w.Body.String())
	}
	var opPair apigen.AuthLoginResult
	if err := json.Unmarshal(w.Body.Bytes(), &opPair); err != nil {
		t.Fatal(err)
	}
	admin, _, _ := store.GetByUsername(context.Background(), "admin")
	adminTok := IssueTokenFor(admin.Username, admin.ID, admin.Roles, admin.TokenVersion)
	body := `{"displayName":"运维二号","roles":["user"]}`
	if w := doJSON(r, http.MethodPatch, "/api/v1/users/"+op.ID, adminTok, body); w.Code != http.StatusOK {
		t.Fatalf("update %d: %s", w.Code, w.Body.String())
	}
	if w := doJSON(r, http.MethodGet, "/api/v1/dashboard", opPair.AccessToken, ""); w.Code != http.StatusOK {
		t.Fatalf("仅改显示名不应吊销会话: %d", w.Code)
	}
}

func TestRevokedAny_批量检查(t *testing.T) {
	bl := NewTokenBlacklist()
	bl.Add("jti-1", time.Now().Add(time.Minute))
	if !bl.RevokedAny("jti-0", "jti-1") {
		t.Fatal("任一命中应返回 true")
	}
	if bl.RevokedAny("jti-0", "jti-2") {
		t.Fatal("均未命中应返回 false")
	}
	bl.Add(SessionKey("s1"), time.Now().Add(-time.Second))
	if bl.RevokedAny(SessionKey("s1")) {
		t.Fatal("过期条目不应命中")
	}
}

func TestSession_过期会话不展示(t *testing.T) {
	store := NewMemSessionStore()
	svc := NewSessionService(store, NewTokenBlacklist())
	u := UserRecord{User: User{ID: "u1", Username: "u"}}
	s := svc.NewSession(u, "", "", time.Now().Add(-time.Second))
	svc.Create(context.Background(), s)
	got, err := svc.ListByUser(context.Background(), "u1")
	if err != nil || len(got) != 0 {
		t.Fatalf("过期会话不应展示: %+v err=%v", got, err)
	}
}

func TestSessionStore_Touch与吊销(t *testing.T) {
	store := NewMemSessionStore()
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second)
	s := Session{ID: "s1", UserID: "u1", Username: "u", CreatedAt: base, LastSeenAt: base, ExpiresAt: base.Add(time.Hour)}
	if err := store.Create(ctx, s); err != nil {
		t.Fatal(err)
	}
	later := base.Add(5 * time.Minute)
	if err := store.Touch(ctx, "s1", later, base.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, _, _ := store.Get(ctx, "s1")
	if !got.LastSeenAt.Equal(later) || !got.ExpiresAt.Equal(base.Add(2*time.Hour)) {
		t.Fatalf("Touch 未生效: %+v", got)
	}
	if err := store.Revoke(ctx, "s1", later); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := store.Get(ctx, "s1"); got.RevokedAt == nil {
		t.Fatal("吊销时间未写入")
	}
	if list, _ := store.ListByUser(ctx, "u1"); len(list) != 0 {
		t.Fatalf("已吊销会话不应展示: %+v", list)
	}
}

func TestSession_无服务时登录仍可用(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewMemUserStore()
	if err := EnsureBootstrap(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	h := NewAuthHandler(store, NewTokenBlacklist(), nil)
	r := gin.New()
	r.POST("/api/v1/auth/login", h.AuthLogin)
	w := doJSON(r, http.MethodPost, "/api/v1/auth/login", "", `{"username":"admin","password":"admin123"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("无会话服务时登录应可用: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"sid"`) {
		t.Fatal("响应不应暴露 sid")
	}
}
