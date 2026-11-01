package platform

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func makeLicense(t *testing.T, priv ed25519.PrivateKey, p LicensePayload) string {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(priv, raw)
	env := LicenseEnvelope{
		Payload:   base64.RawURLEncoding.EncodeToString(raw),
		Signature: base64.RawURLEncoding.EncodeToString(sig),
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLicenseStatusAndActivate(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "license.json")
	svc := NewLicenseService(file, base64.StdEncoding.EncodeToString(pub))
	svc.now = func() time.Time { return time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC) }

	exp := svc.Status(context.Background())
	if exp.Licensed || exp.Edition != "community" {
		t.Fatalf("empty: %+v", exp)
	}

	expires := time.Date(2027, 10, 30, 0, 0, 0, 0, time.UTC)
	raw := makeLicense(t, priv, LicensePayload{
		LicenseID: "lic-1", IssuedTo: "ACME", Edition: "enterprise",
		Features: []string{"ha", "ldap"}, MaxTerminals: 5000,
		IssuedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), ExpiresAt: &expires,
	})
	st, err := svc.Activate(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Licensed || st.Edition != "enterprise" || st.MaxTerminals != 5000 || len(st.Features) != 2 {
		t.Fatalf("activated: %+v", st)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("license file not written: %v", err)
	}

	// 篡改签名
	var env LicenseEnvelope
	_ = json.Unmarshal([]byte(raw), &env)
	env.Signature = base64.RawURLEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
	bad, _ := json.Marshal(env)
	if _, err := svc.Activate(context.Background(), string(bad)); err == nil {
		t.Fatal("tampered signature should fail")
	}

	// 过期（已写文件）
	expired := makeLicense(t, priv, LicensePayload{
		LicenseID: "lic-old", IssuedTo: "ACME", Edition: "standard",
		IssuedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), ExpiresAt: &time.Time{},
	})
	// 显式写入过期授权，状态应为未授权
	if err := os.WriteFile(file, []byte(expired), 0o600); err != nil {
		t.Fatal(err)
	}
	st = svc.Status(context.Background())
	if st.Licensed || st.Message == "" {
		t.Fatalf("expired status: %+v", st)
	}

	if err := svc.Remove(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("license should be removed: %v", err)
	}
}

func TestLicenseWithoutPublicKey(t *testing.T) {
	svc := NewLicenseService(filepath.Join(t.TempDir(), "x.json"), "")
	if st := svc.Status(context.Background()); st.Licensed {
		t.Fatal("no pubkey should be community")
	}
	if _, err := svc.Activate(context.Background(), "{}"); err == nil {
		t.Fatal("activate without pubkey should fail")
	}
}

func TestLicenseTerminalUsage(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "license.json")
	svc := NewLicenseService(file, base64.StdEncoding.EncodeToString(pub))
	svc.now = func() time.Time { return time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC) }

	expires := time.Date(2027, 10, 30, 0, 0, 0, 0, time.UTC)
	raw := makeLicense(t, priv, LicensePayload{
		LicenseID: "lic-u", IssuedTo: "ACME", Edition: "standard",
		MaxTerminals: 10, IssuedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), ExpiresAt: &expires,
	})
	if _, err := svc.Activate(context.Background(), raw); err != nil {
		t.Fatal(err)
	}

	svc.SetTerminalCounter(func(context.Context) (int64, error) { return 12, nil })
	st := svc.Status(context.Background())
	if st.TerminalsUsed != 12 || !st.TerminalsOverLimit {
		t.Fatalf("over-limit: %+v", st)
	}
	if !strings.Contains(st.Message, "超授权上限") {
		t.Fatalf("message=%q", st.Message)
	}

	svc.SetTerminalCounter(func(context.Context) (int64, error) { return 5, nil })
	st = svc.Status(context.Background())
	if st.TerminalsOverLimit || !strings.Contains(st.Message, "5/10") {
		t.Fatalf("under-limit: %+v", st)
	}

	// 计数源故障：保留上限信息，不把状态判为未授权。
	svc.SetTerminalCounter(func(context.Context) (int64, error) { return 0, errors.New("clickhouse down") })
	st = svc.Status(context.Background())
	if !st.Licensed || !strings.Contains(st.Message, "统计暂不可用") {
		t.Fatalf("counter-error: %+v", st)
	}
}
