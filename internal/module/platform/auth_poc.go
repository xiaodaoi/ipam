// Package platform 认证会话（M5-001 打通 → M5-002 JWT 正式化）。
// 响应形状与 vben UserInfo 保持对齐；签名实现已替换为标准 HS256 JWT（auth_jwt.go）。
package platform

import (
	"os"
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

// IssueToken 签发标准 JWT（保持 M5-001 函数名，调用方零改动）。
func IssueToken(now time.Time) string {
	return IssueJWT(JWTClaims{
		Sub: pocUsername, UID: pocUserID,
		Roles: []string{"admin"}, Typ: "user",
	}, now)
}

// IssueTokenFor 按用户记录签发 JWT（M5-004/M5-010：角色+会话版本进 claims）。
func IssueTokenFor(sub, uid string, roles []string, ver int) string {
	return IssueJWT(JWTClaims{Sub: sub, UID: uid, Roles: roles, Typ: "user", Ver: ver}, timeNowUTC())
}

// ValidateToken 校验并返回 claims（含 Sub/Roles/Typ）。
func ValidateToken(tok string) (string, error) {
	c, err := ParseJWT(tok)
	if err != nil {
		return "", err
	}
	return c.UID, nil
}
