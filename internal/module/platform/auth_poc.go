// Package platform 认证 PoC（M5-001）：最小可用的登录/会话链路。
// 正式 JWT/RBAC/Bot Token 由 M5-002 替换（§12.3）；本实现刻意保持
// 响应形状与 vben UserInfo 对齐，升级时仅换签发与校验内部。
package platform

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	pocUserID     = "1"
	pocUsername   = "admin"
	tokenLifetime = 24 * time.Hour
)

func pocPassword() string {
	if p := os.Getenv("IPAM_POC_PASSWORD"); p != "" {
		return p
	}
	return "admin123"
}

func pocSecret() []byte {
	if s := os.Getenv("IPAM_POC_SECRET"); s != "" {
		return []byte(s)
	}
	return []byte("ipam-poc-secret")
}

// IssueToken 签发 "poc.<base64(userId).exp>.<hmac>"（无状态，服务端零存储）。
func IssueToken(now time.Time) string {
	exp := now.Add(tokenLifetime).Unix()
	payload := pocUserID + "." + strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, pocSecret())
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))[:16]
	return "poc." + payload + "." + sig
}

// ValidateToken 校验签名与有效期；返回 userId。
func ValidateToken(tok string) (string, error) {
	parts := strings.Split(tok, ".")
	if len(parts) != 4 || parts[0] != "poc" {
		return "", errors.New("令牌格式非法")
	}
	payload := parts[1] + "." + parts[2]
	mac := hmac.New(sha256.New, pocSecret())
	mac.Write([]byte(payload))
	if !hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))[:16]), []byte(parts[3])) {
		return "", errors.New("令牌签名不匹配")
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return "", errors.New("令牌已过期")
	}
	return parts[1], nil
}
