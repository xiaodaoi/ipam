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
	t.Setenv("IPAM_JWT_SECRET", "other-env-secret")
	resetJWTSecretCacheForTest()
	t.Cleanup(resetJWTSecretCacheForTest)
	if _, err := ParseJWT(tok); err == nil {
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

// Given 未显式指定 jti，When 两次同参签发，Then 各自自动获得互异 jti（M5-032 黑名单键）。
func TestJWTIssue_自动jti(t *testing.T) {
	t1 := IssueJWT(JWTClaims{Sub: "a", UID: "1"}, time.Now())
	t2 := IssueJWT(JWTClaims{Sub: "a", UID: "1"}, time.Now())
	c1, err1 := ParseJWT(t1)
	c2, err2 := ParseJWT(t2)
	if err1 != nil || err2 != nil {
		t.Fatalf("parse: %v/%v", err1, err2)
	}
	if c1.JTI == "" || c2.JTI == "" {
		t.Fatal("jti 应自动生成")
	}
	if c1.JTI == c2.JTI || t1 == t2 {
		t.Fatal("同参两次签发的 jti/令牌应互异")
	}
}

// Given 已过期令牌，When 以 checkExp=false 解析，Then 验签通过返回 claims（刷新降级路径用）。
func TestParseJWT_容过期变体(t *testing.T) {
	tok := IssueJWT(JWTClaims{Sub: "a", UID: "1", Exp: time.Now().Add(-time.Minute).Unix()}, time.Now())
	if _, err := ParseJWT(tok); err != errTokenExpired {
		t.Fatalf("严格解析应报过期: %v", err)
	}
	c, err := parseJWT(tok, false)
	if err != nil || c.Sub != "a" {
		t.Fatalf("容过期解析应通过: %v %+v", err, c)
	}
}

// Given 环境变量设置情况各异，When 解析签名密钥，Then env 优先、均未设时随机生成（M5-032）。
func TestResolveJWTSecret_env优先与随机兜底(t *testing.T) {
	t.Setenv("IPAM_JWT_SECRET", "s-jwt")
	t.Setenv("IPAM_POC_SECRET", "s-poc")
	if sec, rnd := resolveJWTSecret(); rnd || string(sec) != "s-jwt" {
		t.Fatalf("IPAM_JWT_SECRET 应优先: %q rnd=%v", sec, rnd)
	}
	t.Setenv("IPAM_JWT_SECRET", "")
	if sec, rnd := resolveJWTSecret(); rnd || string(sec) != "s-poc" {
		t.Fatalf("IPAM_POC_SECRET 应兜底: %q rnd=%v", sec, rnd)
	}
	t.Setenv("IPAM_POC_SECRET", "")
	s1, r1 := resolveJWTSecret()
	s2, r2 := resolveJWTSecret()
	if !r1 || !r2 {
		t.Fatal("均未设置时应标记随机生成")
	}
	if len(s1) != 64 {
		t.Fatalf("32 字节 hex 应为 64 字符: %d", len(s1))
	}
	if string(s1) == "ipam-dev-secret" || string(s1) == string(s2) {
		t.Fatal("随机密钥不得为硬编码值且两次生成应互异")
	}
}

// Given 未设 env，When 经缓存包装取钥，Then 进程内稳定且非硬编码（重启失效由告警承载）。
func TestJwtSecret_进程内稳定且非硬编码(t *testing.T) {
	t.Setenv("IPAM_JWT_SECRET", "")
	t.Setenv("IPAM_POC_SECRET", "")
	resetJWTSecretCacheForTest()
	t.Cleanup(resetJWTSecretCacheForTest)
	a := jwtSecret()
	if string(a) != string(jwtSecret()) {
		t.Fatal("进程内密钥应稳定（sync.Once 缓存）")
	}
	if string(a) == "ipam-dev-secret" {
		t.Fatal("禁止硬编码兜底密钥")
	}
}
