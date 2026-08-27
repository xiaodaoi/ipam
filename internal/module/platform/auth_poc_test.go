package platform

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
)

func newAuthRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/v1/auth/login", func(c *gin.Context) { NewAuthHandler().AuthLogin(c) })
	r.GET("/api/v1/auth/user/info", func(c *gin.Context) { NewAuthHandler().GetAuthUserInfo(c) })
	return r
}

func login(t *testing.T, r http.Handler) string {
	t.Helper()
	body := `{"username":"admin","password":"admin123"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("login %d: %s", w.Code, w.Body.String())
	}
	var res apigen.AuthLoginResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	return res.AccessToken
}

func TestLoginIssueAndUserInfo(t *testing.T) {
	r := newAuthRouter()

	// 错误口令 401
	w401 := httptest.NewRecorder()
	r.ServeHTTP(w401, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"username":"admin","password":"wrong"}`)))
	if w401.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w401.Code)
	}

	token := login(t, r)
	if !strings.HasPrefix(token, "poc.") {
		t.Fatalf("token shape: %s", token)
	}

	// 无 token 401
	wNo := httptest.NewRecorder()
	r.ServeHTTP(wNo, httptest.NewRequest(http.MethodGet, "/api/v1/auth/user/info", nil))
	if wNo.Code != http.StatusUnauthorized {
		t.Fatalf("no-token want 401, got %d", wNo.Code)
	}

	// 携带 token 拿 UserInfo
	wOK := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/user/info", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(wOK, req)
	if wOK.Code != 200 {
		t.Fatalf("userinfo %d: %s", wOK.Code, wOK.Body.String())
	}
	var info apigen.UserInfo
	if err := json.Unmarshal(wOK.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.UserId != "1" || len(info.Roles) != 1 || info.Roles[0] != "admin" || info.HomePath != "/overview" {
		t.Fatalf("userinfo 异常: %+v", info)
	}
}

func TestTokenTamperRejected(t *testing.T) {
	r := newAuthRouter()
	token := login(t, r)

	// 篡改签名位
	bad := token[:len(token)-2] + "xx"
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/user/info", nil)
	req.Header.Set("Authorization", "Bearer "+bad)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("tampered want 401, got %d", w.Code)
	}
}
