package platform

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
)

// AuthHandler 实现 apigen.ServerInterface 中 auth 域端点（M5-004/M5-011：bcrypt 校验+登出吊销；
// M5-032：登录限流、refresh 轮换、双令牌登出吊销）。
type AuthHandler struct {
	users      UserStore
	bl         *TokenBlacklist
	permLookup func(ctx context.Context, role string) ([]string, bool)
	loginLimit *loginRateLimiter
}

func NewAuthHandler(users UserStore, bl *TokenBlacklist, permLookup func(ctx context.Context, role string) ([]string, bool)) *AuthHandler {
	return &AuthHandler{users: users, bl: bl, permLookup: permLookup,
		loginLimit: newLoginRateLimiter(func() time.Time { return timeNowUTC() })}
}

// AuthLogin POST /auth/login
func (h *AuthHandler) AuthLogin(c *gin.Context) {
	// M5-032：每 IP 5 次/分钟限流（在口令校验前计数，超限直接 429）
	if allowed, retryAfter := h.loginLimit.allow(c.ClientIP()); !allowed {
		c.Header("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
		WriteProblem(c, http.StatusTooManyRequests, "https://ipam.local/problems/rate-limit",
			"LOGIN_RATE_LIMITED", "登录尝试过于频繁，请稍后重试")
		return
	}
	var body apigen.AuthLoginRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		writeAuthErr(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	rec, ok, err := h.users.GetByUsername(c.Request.Context(), body.Username)
	if err != nil {
		WriteProblem(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	if !ok || !rec.Enabled || !CheckPassword(rec.PasswordHash, body.Password) {
		writeAuthErr(c, http.StatusUnauthorized, "BAD_CREDENTIALS", "账号或口令错误")
		return
	}
	access, refresh := issueAuthPair(rec)
	c.JSON(http.StatusOK, apigen.AuthLoginResult{AccessToken: access, RefreshToken: refresh})
}

// issueAuthPair 签发访问+刷新令牌对（M5-032）。
func issueAuthPair(rec UserRecord) (access string, refresh string) {
	return IssueTokenFor(rec.Username, rec.ID, rec.Roles, rec.TokenVersion),
		IssueRefreshTokenFor(rec.Username, rec.ID, rec.TokenVersion)
}

// AuthRefresh POST /auth/refresh（M5-032：轮换制刷新）。
// 主路径：请求体 refreshToken（kind=refresh，验签+过期+黑名单，旧 jti 先吊销再签发）。
// 降级路径：Authorization Bearer 访问令牌——验签（容过期）+ 黑名单 + 过期后 7 天宽限窗口。
func (h *AuthHandler) AuthRefresh(c *gin.Context) {
	var body apigen.AuthRefreshRequest
	// 请求体可选（降级路径可仅带 Authorization 头）；空体（EOF）不视为错误
	if err := c.ShouldBindJSON(&body); err != nil && !errors.Is(err, io.EOF) {
		writeAuthErr(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	if body.RefreshToken != nil && *body.RefreshToken != "" {
		h.refreshByRefreshToken(c, *body.RefreshToken)
		return
	}
	h.refreshByAccessToken(c)
}

// refreshByRefreshToken 刷新令牌主路径：严格校验 + 轮换吊销。
func (h *AuthHandler) refreshByRefreshToken(c *gin.Context, tok string) {
	claims, err := ParseJWT(tok)
	if err != nil {
		writeAuthErr(c, http.StatusUnauthorized, "TOKEN_INVALID", err.Error())
		return
	}
	if claims.Kind != tokenKindRefresh {
		writeAuthErr(c, http.StatusUnauthorized, "TOKEN_INVALID", "非刷新令牌")
		return
	}
	if h.bl != nil && h.bl.Revoked(claims.JTI) {
		writeAuthErr(c, http.StatusUnauthorized, "TOKEN_REVOKED", "刷新令牌已吊销")
		return
	}
	rec, ok := h.refreshUser(c, claims)
	if !ok {
		return
	}
	// 轮换：旧刷新令牌 jti 先吊销（至其自然过期），再签发新对——防重放
	if h.bl != nil {
		h.bl.Add(claims.JTI, time.Unix(claims.Exp, 0))
	}
	access, refresh := issueAuthPair(rec)
	c.JSON(http.StatusOK, apigen.AuthRefreshResult{AccessToken: access, RefreshToken: refresh})
}

// refreshByAccessToken 降级路径：访问令牌（容过期，限过期后 7 天宽限窗口）换新对。
// 旧访问令牌不吊销（存活期 ≤24h 且可能有在途请求；重放窗口受 refreshTokenLifetime 约束）。
func (h *AuthHandler) refreshByAccessToken(c *gin.Context) {
	tok := bearerOf(c.GetHeader("Authorization"))
	if tok == "" {
		writeAuthErr(c, http.StatusUnauthorized, "TOKEN_MISSING", "未携带刷新令牌或访问令牌")
		return
	}
	claims, err := parseJWT(tok, false)
	if err != nil {
		writeAuthErr(c, http.StatusUnauthorized, "TOKEN_INVALID", err.Error())
		return
	}
	if claims.Kind == tokenKindRefresh {
		// Authorization 头携带刷新令牌：转主路径语义（严格过期+黑名单+轮换）
		if time.Now().Unix() > claims.Exp {
			writeAuthErr(c, http.StatusUnauthorized, "TOKEN_INVALID", errTokenExpired.Error())
			return
		}
		h.refreshByRefreshToken(c, tok)
		return
	}
	if time.Now().Unix() > claims.Exp+int64(refreshTokenLifetime.Seconds()) {
		writeAuthErr(c, http.StatusUnauthorized, "TOKEN_INVALID", "访问令牌已过刷新宽限窗口，请重新登录")
		return
	}
	if h.bl != nil && h.bl.Revoked(blacklistKey(claims, tok)) {
		writeAuthErr(c, http.StatusUnauthorized, "TOKEN_REVOKED", "令牌已吊销（已登出）")
		return
	}
	rec, ok := h.refreshUser(c, claims)
	if !ok {
		return
	}
	access, refresh := issueAuthPair(rec)
	c.JSON(http.StatusOK, apigen.AuthRefreshResult{AccessToken: access, RefreshToken: refresh})
}

// refreshUser 刷新前用户态校验：存在/启用/会话版本匹配（M5-010 语义延续到刷新链路）。
func (h *AuthHandler) refreshUser(c *gin.Context, claims JWTClaims) (UserRecord, bool) {
	rec, ok, err := h.users.GetByID(c.Request.Context(), claims.UID)
	if err != nil {
		WriteProblem(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return UserRecord{}, false
	}
	if !ok || !rec.Enabled || (claims.Ver != 0 && claims.Ver != rec.TokenVersion) {
		writeAuthErr(c, http.StatusUnauthorized, "TOKEN_REVOKED", "账号不可用或会话已失效")
		return UserRecord{}, false
	}
	return rec, true
}

// GetAuthUserInfo GET /auth/user/info
func (h *AuthHandler) GetAuthUserInfo(c *gin.Context) {
	claims, err := ParseJWT(bearerOf(c.GetHeader("Authorization")))
	if err != nil {
		writeAuthErr(c, http.StatusUnauthorized, "TOKEN_INVALID", err.Error())
		return
	}
	realName := claims.Sub
	if rec, ok, lerr := h.users.GetByID(c.Request.Context(), claims.UID); lerr == nil && ok && rec.DisplayName != "" {
		realName = rec.DisplayName
	}
	// M2-035：权限点集合（hasPerm 逐点过滤，含自定义角色 permLookup）
	perms := make([]string, 0, len(allPermList))
	for _, p := range allPermList {
		if hasPerm(claims, p, h.permLookup, c.Request.Context()) {
			perms = append(perms, p)
		}
	}
	c.JSON(http.StatusOK, apigen.UserInfo{
		UserId:      claims.UID,
		Username:    claims.Sub,
		RealName:    realName,
		Roles:       claims.Roles,
		Permissions: &perms,
		Desc:        strPtr("IPAM 控制面账号"),
		HomePath:    "/overview",
	})
}

// ListAuthCodes GET /auth/codes（PoC 空=按钮级权限不启用）
func (h *AuthHandler) ListAuthCodes(c *gin.Context) {
	claims, err := ParseJWT(bearerOf(c.GetHeader("Authorization")))
	if err != nil {
		writeAuthErr(c, http.StatusUnauthorized, "TOKEN_INVALID", err.Error())
		return
	}
	// M2-035：权限点集合（前端 accessCodes → 菜单/按钮过滤）
	perms := make([]string, 0, len(allPermList))
	for _, p := range allPermList {
		if hasPerm(claims, p, h.permLookup, c.Request.Context()) {
			perms = append(perms, p)
		}
	}
	c.JSON(http.StatusOK, perms)
}

// LogoutAuth POST /auth/logout（M5-011：登出即吊销——有效令牌入黑名单至自然过期；
// M5-032：请求体携带 refreshToken 时双令牌 jti 一并吊销）。
func (h *AuthHandler) LogoutAuth(c *gin.Context) {
	ok := true
	if tok := bearerOf(c.GetHeader("Authorization")); tok != "" {
		if claims, perr := ParseJWT(tok); perr == nil {
			h.bl.Add(blacklistKey(claims, tok), time.Unix(claims.Exp, 0))
		}
	}
	var body apigen.AuthLogoutRequest
	if err := c.ShouldBindJSON(&body); err == nil && body.RefreshToken != nil && *body.RefreshToken != "" {
		if claims, perr := ParseJWT(*body.RefreshToken); perr == nil {
			h.bl.Add(blacklistKey(claims, *body.RefreshToken), time.Unix(claims.Exp, 0))
		}
	}
	c.JSON(http.StatusOK, apigen.AuthLogoutDone{Ok: &ok})
}

func bearerOf(header string) string {
	if v, ok := strings.CutPrefix(header, "Bearer "); ok {
		return strings.TrimSpace(v)
	}
	return header
}

var timeNowUTC = time.Now

func strPtr(s string) *string { return &s }

func writeAuthErr(c *gin.Context, status int, code, detail string) {
	WriteProblem(c, status, "https://ipam.local/problems/unauthorized", code, detail)
}

// GetAuthUserInfoAlias GET /user/info（vben 底座约定路径，与 GetAuthUserInfo 同一资源）
func (h *AuthHandler) GetAuthUserInfoAlias(c *gin.Context) {
	h.GetAuthUserInfo(c)
}
