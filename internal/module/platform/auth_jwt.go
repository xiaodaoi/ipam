package platform

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// JWTClaims 令牌载荷（M5-002）。
// roles：admin 管理级 / user 只读级；typ：user 人工 / bot 机器（§12.3）。
type JWTClaims struct {
	Sub   string   `json:"sub"` // 账号
	UID   string   `json:"uid"`
	Roles []string `json:"roles"`
	Typ   string   `json:"typ"`           // user | bot
	Ver   int      `json:"ver,omitempty"` // 会话版本（M5-010：0=存量令牌，按 1 兼容）
	Exp   int64    `json:"exp"`
}

var (
	errTokenMalformed = errors.New("令牌格式非法")
	errTokenBadSig    = errors.New("令牌签名不匹配")
	errTokenExpired   = errors.New("令牌已过期")
)

func jwtSecret() []byte {
	if s := os.Getenv("IPAM_JWT_SECRET"); s != "" {
		return []byte(s)
	}
	if s := os.Getenv("IPAM_POC_SECRET"); s != "" {
		return []byte(s)
	}
	return []byte("ipam-dev-secret")
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func signHS256(payload []byte) []byte {
	m := hmac.New(sha256.New, jwtSecret())
	m.Write(payload)
	return m.Sum(nil)
}

// IssueJWT 签发 HS256 JWT（header 固定 {"alg":"HS256","typ":"JWT"}）。
func IssueJWT(c JWTClaims, now time.Time) string {
	if c.Exp == 0 {
		c.Exp = now.Add(tokenLifetime).Unix()
	}
	header := b64url([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body, _ := json.Marshal(c)
	payload := header + "." + b64url(body)
	return payload + "." + b64url(signHS256([]byte(payload)))
}

// ParseJWT 验签+过期校验，返回 claims。
func ParseJWT(tok string) (JWTClaims, error) {
	var c JWTClaims
	parts := strings.Split(tok, ".")
	if len(parts) != 3 || parts[0] != b64url([]byte(`{"alg":"HS256","typ":"JWT"}`)) {
		return c, errTokenMalformed
	}
	want := signHS256([]byte(parts[0] + "." + parts[1]))
	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(want, got) {
		return c, errTokenBadSig
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || json.Unmarshal(body, &c) != nil {
		return c, errTokenMalformed
	}
	if time.Now().Unix() > c.Exp {
		return c, errTokenExpired
	}
	return c, nil
}

// HasRole 角色判定（admin 蕴含 user 权限）。
func HasRole(c JWTClaims, role string) bool {
	for _, r := range c.Roles {
		if r == role || (r == "admin" && role == "user") {
			return true
		}
	}
	return false
}

// ActorTypeOf §12.3 调用者类型映射。
func ActorTypeOf(c JWTClaims) string {
	if c.Typ == "bot" {
		return "bot"
	}
	return "human"
}

// JWTActorProvider 从 Authorization Bearer 解析 JWT，输出审计 actor 三元组
// （actorType, actor, tokenSub）。令牌缺失/无效回退 system（审计不阻断业务，M4-003 语义保持）。
func JWTActorProvider(c *gin.Context) (string, string, string) {
	return JWTActorFromHeader(c.GetHeader("Authorization"))
}

// JWTActorFromHeader 同上，直接吃 Authorization 头（便于无 gin 桩测试）。
func JWTActorFromHeader(header string) (string, string, string) {
	tok := strings.TrimPrefix(header, "Bearer ")
	claims, err := ParseJWT(strings.TrimSpace(tok))
	if err != nil {
		return "system", "control-plane", ""
	}
	return ActorTypeOf(claims), claims.Sub, "jwt:" + claims.Sub + "#" + shortHash(tok)
}

func shortHash(s string) string {
	sum := signHS256([]byte(s))
	const digits = "0123456789abcdef"
	out := make([]byte, 0, 16)
	for _, x := range sum[:8] {
		out = append(out, digits[x>>4], digits[x&0x0f])
	}
	return string(out)
}
