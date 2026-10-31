package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	guuid "github.com/google/uuid"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
)

func TestGenerateRecoveryCodes(t *testing.T) {
	codes, hashes, err := GenerateRecoveryCodes(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 10 || len(hashes) != 10 {
		t.Fatalf("数量错误: %d/%d", len(codes), len(hashes))
	}
	pat := regexp.MustCompile(`^[A-Z2-9]{5}-[A-Z2-9]{5}$`)
	seen := map[string]bool{}
	for i, c := range codes {
		if !pat.MatchString(c) {
			t.Fatalf("格式错误: %q", c)
		}
		if seen[c] {
			t.Fatalf("重复恢复码: %q", c)
		}
		seen[c] = true
		if hashes[i] != RecoveryCodeHash(c) {
			t.Fatalf("散列不一致: %q", c)
		}
	}
	// 大小写/分隔符不敏感
	rest, ok := ConsumeRecoveryCode(hashes, " "+toLowerNoDash(codes[0])+" ")
	if !ok || len(rest) != 9 {
		t.Fatalf("消耗失败: ok=%v left=%d", ok, len(rest))
	}
	// 已消耗的码不能再用
	if _, ok := ConsumeRecoveryCode(rest, codes[0]); ok {
		t.Fatal("已使用的恢复码不应再次通过")
	}
	// 形状不符直接拒绝
	if _, ok := ConsumeRecoveryCode(hashes, "123"); ok {
		t.Fatal("长度不符应失败")
	}
}

func toLowerNoDash(code string) string {
	out := make([]rune, 0, len(code))
	for _, r := range code {
		if r == '-' {
			continue
		}
		if r >= 'A' && r <= 'Z' {
			r = r + 32
		}
		out = append(out, r)
	}
	return string(out)
}

func TestRecoveryCodeFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewMemUserStore()
	if err := EnsureBootstrap(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	authH := NewAuthHandler(store, NewTokenBlacklist(), nil)
	r := gin.New()
	r.POST("/api/v1/auth/login", authH.AuthLogin)
	r.GET("/api/v1/auth/2fa/recovery-codes", authH.GetTotpRecoveryStatus)
	r.POST("/api/v1/auth/2fa/recovery-codes", authH.GenerateTotpRecoveryCodes)

	admin, _, _ := store.GetByUsername(context.Background(), "admin")
	secret, _ := GenerateTOTPSecret()
	enabled := true
	if _, err := store.Update(context.Background(), admin.ID, UserUpdateInput{TOTPSecret: &secret, TOTPEnabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	code, _ := totpCodeAt(secret, time.Now())
	w, login := loginWithRaw(t, r, `{"username":"admin","password":"admin123","totpCode":"`+code+`"}`)
	if w.Code != http.StatusOK || login.AccessToken == "" {
		t.Fatalf("启用 TOTP 后应可用动态口令登录: %d %s", w.Code, w.Body.String())
	}
	tok := login.AccessToken

	// 未通过身份确认不能生成
	if w := doJSON(r, http.MethodPost, "/api/v1/auth/2fa/recovery-codes", tok, `{"password":"wrong"}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("错误口令应 401，实际 %d", w.Code)
	}

	code, _ = totpCodeAt(secret, time.Now())
	w = doJSON(r, http.MethodPost, "/api/v1/auth/2fa/recovery-codes", tok, `{"totpCode":"`+code+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("生成恢复码失败 %d: %s", w.Code, w.Body.String())
	}
	var gen apigen.TotpRecoveryCodes
	if err := json.Unmarshal(w.Body.Bytes(), &gen); err != nil {
		t.Fatal(err)
	}
	if len(gen.Codes) != 10 || gen.Remaining != 10 {
		t.Fatalf("恢复码数量错误: %+v", gen)
	}

	// 该请求不带 totpCode → TOTP_REQUIRED；带恢复码 → 通过
	req := `{"username":"admin","password":"admin123","totpCode":"` + gen.Codes[0] + `"}`
	w, res := loginWithRaw(t, r, req)
	if w.Code != http.StatusOK || res.AccessToken == "" {
		t.Fatalf("恢复码登录失败 %d: %s", w.Code, w.Body.String())
	}
	// 同一恢复码不能复用
	if w, _ := loginWithRaw(t, r, req); w.Code != http.StatusUnauthorized {
		t.Fatalf("恢复码复用应 401，实际 %d", w.Code)
	}

	// 剩余数量应减 1
	w = doJSON(r, http.MethodGet, "/api/v1/auth/2fa/recovery-codes", res.AccessToken, "")
	var st apigen.TotpRecoveryStatus
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || st.Remaining != 9 {
		t.Fatalf("剩余恢复码应为 9: %d %+v", w.Code, st)
	}
}

func loginWithRaw(t *testing.T, r http.Handler, body string) (*httptest.ResponseRecorder, apigen.AuthLoginResult) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	var res apigen.AuthLoginResult
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	return w, res
}

// TestAdminResetTotp 管理员重置 2FA：清空密钥/恢复码并吊销存量会话。
func TestAdminResetTotp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewMemUserStore()
	if err := EnsureBootstrap(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	admin, _, _ := store.GetByUsername(context.Background(), "admin")
	u, err := store.Create(context.Background(), UserCreateInput{
		Username: "lostphone", Password: "User-Passw0rd!23", Roles: []string{"user"},
	})
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := GenerateTOTPSecret()
	enabled := true
	_, hashes, _ := GenerateRecoveryCodes(3)
	if _, err := store.Update(context.Background(), u.ID, UserUpdateInput{
		TOTPSecret: &secret, TOTPEnabled: &enabled, TOTPRecovery: &hashes,
	}); err != nil {
		t.Fatal(err)
	}

	h := NewUserHandler(store)
	r := gin.New()
	r.PUT("/api/v1/users/:userId", func(c *gin.Context) {
		id, err := guuid.Parse(c.Param("userId"))
		if err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		h.UpdateUser(c, id)
	})
	tok := IssueTokenFor(admin.Username, admin.ID, admin.Roles, admin.TokenVersion)
	w := doJSON(r, http.MethodPut, "/api/v1/users/"+u.ID, tok, `{"totpReset":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("重置 2FA 失败 %d: %s", w.Code, w.Body.String())
	}
	got, _, _ := store.GetByUsername(context.Background(), "lostphone")
	if got.TOTPEnabled || got.TOTPSecret != "" || len(got.TOTPRecovery) != 0 {
		t.Fatalf("TOTP 未清空: %+v", got.User)
	}
	if got.TokenVersion <= u.TokenVersion {
		t.Fatalf("应吊销旧会话: %d", got.TokenVersion)
	}
}
