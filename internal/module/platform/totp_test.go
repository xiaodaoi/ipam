package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTOTPCodeAndValidate(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	code, err := totpCodeAt(secret, now)
	if err != nil || len(code) != 6 {
		t.Fatalf("code=%q err=%v", code, err)
	}
	if !ValidateTOTP(secret, code, now) {
		t.Fatal("current code should validate")
	}
	if !ValidateTOTP(secret, code, now.Add(30*time.Second)) {
		t.Fatal("previous window should validate")
	}
	if ValidateTOTP(secret, code, now.Add(2*time.Minute)) {
		t.Fatal("outside window should fail")
	}
	if ValidateTOTP(secret, "000000", now) && code != "000000" {
		t.Fatal("wrong code should fail")
	}
}

func TestLoginRequiresTOTP(t *testing.T) {
	r, store := newPasswordRouter(t)
	rec, _, _ := store.GetByUsername(context.Background(), "admin")
	secret, _ := GenerateTOTPSecret()
	if _, err := store.Update(context.Background(), rec.ID, UserUpdateInput{
		TOTPSecret: &secret, TOTPEnabled: boolPtr(true),
	}); err != nil {
		t.Fatal(err)
	}

	// 无动态口令
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"username":"admin","password":"admin123"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "TOTP_REQUIRED") {
		t.Fatalf("want TOTP_REQUIRED, got %d %s", w.Code, w.Body.String())
	}

	// 正确动态口令
	code, _ := totpCodeAt(secret, time.Now())
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"username":"admin","password":"admin123","totpCode":"`+code+`"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login with totp %d: %s", w.Code, w.Body.String())
	}
	var res map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res["accessToken"] == nil {
		t.Fatal("token missing")
	}
}
