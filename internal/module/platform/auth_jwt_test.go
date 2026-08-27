package platform

import (
	"strings"
	"testing"
	"time"
)

func TestJWTissueParseRoundTrip(t *testing.T) {
	now := time.Now()
	tok := IssueJWT(JWTClaims{Sub: "admin", UID: "1", Roles: []string{"admin"}, Typ: "user"}, now)
	c, err := ParseJWT(tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if c.Sub != "admin" || c.UID != "1" || !HasRole(c, "admin") || !HasRole(c, "user") {
		t.Fatalf("claims: %+v", c)
	}
}

func TestJWTexpired(t *testing.T) {
	tok := IssueJWT(JWTClaims{Sub: "admin", UID: "1", Roles: []string{"admin"}, Exp: time.Now().Add(-time.Minute).Unix()}, time.Now())
	if _, err := ParseJWT(tok); err != errTokenExpired {
		t.Fatalf("want expired, got %v", err)
	}
}

func TestJWTtampered(t *testing.T) {
	tok := IssueJWT(JWTClaims{Sub: "admin", UID: "1", Roles: []string{"admin"}}, time.Now())
	bad := tok[:len(tok)-3] + "AAAA"
	if _, err := ParseJWT(bad); err == nil {
		t.Fatal("篡改令牌必须被拒")
	}
	// 换密钥即拒（防跨环境令牌重放）
	jwtSecretOrig := jwtSecret()
	t.Setenv("IPAM_JWT_SECRET", "other-env-secret")
	if _, err := ParseJWT(tok); err == nil {
		_ = jwtSecretOrig
		t.Fatal("异密钥令牌必须被拒")
	}
}

func TestJWTwrongKeyRejected(t *testing.T) {
	tok := IssueJWT(JWTClaims{Sub: "x", UID: "2", Roles: []string{"user"}}, time.Now())
	if _, err := ParseJWT(strings.Replace(tok, "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9", "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0", 1)); err != errTokenMalformed && err != errTokenBadSig {
		t.Fatalf("alg 篡改应被拒, got %v", err)
	}
}

func TestActorProvider(t *testing.T) {
	tok := IssueJWT(JWTClaims{Sub: "ops1", UID: "9", Roles: []string{"admin"}, Typ: "bot"}, time.Now())
	hdr := "Bearer " + tok
	at, actor, sub := JWTActorFromHeader(hdr)
	if at != "bot" || actor != "ops1" || !strings.HasPrefix(sub, "jwt:ops1#") {
		t.Fatalf("actor: %s/%s/%s", at, actor, sub)
	}
	// 无效令牌回退 system
	at2, actor2, sub2 := JWTActorFromHeader("Bearer garbage")
	if at2 != "system" || actor2 != "control-plane" || sub2 != "" {
		t.Fatalf("fallback: %s/%s/%s", at2, actor2, sub2)
	}
}
