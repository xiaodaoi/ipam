package platform

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// JWTClaims 令牌载荷（M5-002）。
// roles：admin 管理级 / user 只读级；typ：user 人工 / bot 机器（§12.3）。
type JWTClaims struct {
	Sub   string   `json:"sub"` // 账号
	UID   string   `json:"uid"`
	Roles []string `json:"roles"`
	Typ   string   `json:"typ"`            // user | bot
	Ver   int      `json:"ver,omitempty"`  // 会话版本（M5-010：0=存量令牌，按 1 兼容）
	JTI   string   `json:"jti,omitempty"`  // 令牌唯一标识（M5-032：黑名单键）
	Kind  string   `json:"kind,omitempty"` // 令牌类别（M5-032：refresh=刷新令牌；空=访问令牌）
	SID   string   `json:"sid,omitempty"`  // 会话 ID（M6-007C：会话列表/强制下线）
	Exp   int64    `json:"exp"`
}

// tokenKindRefresh 刷新令牌类别标识（M5-032）。独立于 Typ（用户/机器语义）避免冲突。
const tokenKindRefresh = "refresh"

var (
	errTokenMalformed = errors.New("令牌格式非法")
	errTokenBadSig    = errors.New("令牌签名不匹配")
	errTokenExpired   = errors.New("令牌已过期")
)

var (
	jwtSecretOnce sync.Once
	jwtSecretVal  []byte
)

// jwtSecret 进程级签名密钥：env 优先（IPAM_JWT_SECRET > IPAM_POC_SECRET）；
// 均未设置时启动期生成 32 字节随机密钥（hex 编码）并显著告警——会话不再跨重启存活
// （M5-032：移除硬编码 "ipam-dev-secret" 兜底）。进程内稳定（sync.Once 缓存）。
func jwtSecret() []byte {
	jwtSecretOnce.Do(func() {
		secret, _ := resolveJWTSecret()
		jwtSecretVal = secret
	})
	return jwtSecretVal
}

// resolveJWTSecret 解析签名密钥；返回密钥与是否随机生成（测试可直接调用）。
func resolveJWTSecret() (secret []byte, randomGenerated bool) {
	if s := os.Getenv("IPAM_JWT_SECRET"); s != "" {
		return []byte(s), false
	}
	if s := os.Getenv("IPAM_POC_SECRET"); s != "" {
		return []byte(s), false
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// OS 熵源不可用即进程不可安全运行；仅在启动期首次用钥时触发
		panic("ipam: crypto/rand 不可用，无法生成随机 JWT 密钥: " + err.Error())
	}
	log.Printf("⚠️  IPAM_JWT_SECRET/IPAM_POC_SECRET 均未设置：已生成随机 JWT 签名密钥——" +
		"进程重启后全部会话失效！生产环境请显式配置 IPAM_JWT_SECRET。")
	return []byte(hex.EncodeToString(b)), true
}

// resetJWTSecretCacheForTest 清除密钥缓存（测试切换 env 后调用；t.Cleanup 复位）。
func resetJWTSecretCacheForTest() {
	jwtSecretOnce = sync.Once{}
	jwtSecretVal = nil
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func signHS256(payload []byte) []byte {
	m := hmac.New(sha256.New, jwtSecret())
	m.Write(payload)
	return m.Sum(nil)
}

// IssueJWT 签发 HS256 JWT（header 固定 {"alg":"HS256","typ":"JWT"}）。
// JTI 缺省时自动随机生成（M5-032：访问/刷新令牌均强制携带 jti 供黑名单键用）。
func IssueJWT(c JWTClaims, now time.Time) string {
	if c.Exp == 0 {
		c.Exp = now.Add(tokenLifetime).Unix()
	}
	if c.JTI == "" {
		c.JTI = newJTI()
	}
	header := b64url([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body, _ := json.Marshal(c)
	payload := header + "." + b64url(body)
	return payload + "." + b64url(signHS256([]byte(payload)))
}

// newJTI 生成随机令牌标识（16 字节熵，hex 编码）。
func newJTI() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("ipam: crypto/rand 不可用，无法生成 jti: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// ParseJWT 验签+过期校验，返回 claims。
func ParseJWT(tok string) (JWTClaims, error) {
	return parseJWT(tok, true)
}

// parseJWT 验签并按需校验过期（checkExp=false 供刷新降级路径接受过期访问令牌）。
func parseJWT(tok string, checkExp bool) (JWTClaims, error) {
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
	if checkExp && time.Now().Unix() > c.Exp {
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
