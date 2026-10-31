package platform

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// AuthHandler 实现 apigen.ServerInterface 中 auth 域端点（M5-004/M5-011：bcrypt 校验+登出吊销；
// M5-032：登录限流、refresh 轮换、双令牌登出吊销）。
type AuthHandler struct {
	users      UserStore
	bl         *TokenBlacklist
	permLookup func(ctx context.Context, role string) ([]string, bool)
	loginLimit *loginRateLimiter
	sec        SecuritySettingsStore
	ldap       DirectoryAuthenticator
}

// DirectoryAuthenticator 目录认证抽象（M6-007B）；便于单测注入假实现。
type DirectoryAuthenticator interface {
	Authenticate(ctx context.Context, username, password string) (LDAPUser, error)
}

func NewAuthHandler(users UserStore, bl *TokenBlacklist, permLookup func(ctx context.Context, role string) ([]string, bool), sec ...SecuritySettingsStore) *AuthHandler {
	var store SecuritySettingsStore
	if len(sec) > 0 {
		store = sec[0]
	}
	return &AuthHandler{users: users, bl: bl, permLookup: permLookup, sec: store,
		loginLimit: newLoginRateLimiter(func() time.Time { return timeNowUTC() })}
}

// SetLDAP 注入目录认证（M6-007B）；未注入时仅本地账号登录。
func (h *AuthHandler) SetLDAP(a DirectoryAuthenticator) { h.ldap = a }

// forcePasswordChange 读取全局策略；默认关闭，读取失败时按关闭处理（可用性优先）。
func (h *AuthHandler) forcePasswordChange(ctx context.Context) bool {
	if h.sec == nil {
		return false
	}
	s, err := h.sec.Get(ctx)
	if err != nil {
		return false
	}
	return s.ForcePasswordChange
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
	// 空口令一律拒绝：LDAP 的 simple bind 对空口令可能返回成功（匿名 bind），
	// 必须在进入目录认证前拦截。
	if body.Password == "" {
		SetAuditMeta(c, "login", body.Username, "failed")
		writeAuthErr(c, http.StatusUnauthorized, "BAD_CREDENTIALS", "账号或口令错误")
		return
	}
	rec, ok, err := h.users.GetByUsername(c.Request.Context(), body.Username)
	if err != nil {
		WriteProblem(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	useLDAP := h.ldap != nil && (!ok || rec.AuthSource == "ldap")
	if useLDAP {
		lu, lerr := h.ldap.Authenticate(c.Request.Context(), body.Username, body.Password)
		if lerr != nil {
			switch {
			case errors.Is(lerr, ErrLDAPUnavailable):
				SetAuditMeta(c, "login", body.Username, "ldap_unavailable")
				problem.Write(c, http.StatusServiceUnavailable, "https://ipam.local/problems/ldap-unavailable", "LDAP_UNAVAILABLE", "目录服务不可达")
			case errors.Is(lerr, ErrLDAPDisabled):
				// 未启用目录服务时按口令错误处理，避免暴露认证后端状态。
				SetAuditMeta(c, "login", body.Username, "failed")
				writeAuthErr(c, http.StatusUnauthorized, "BAD_CREDENTIALS", "账号或口令错误")
			default:
				SetAuditMeta(c, "login", body.Username, "failed")
				writeAuthErr(c, http.StatusUnauthorized, "BAD_CREDENTIALS", "账号或口令错误")
			}
			return
		}
		rec, err = h.syncLDAPUser(c.Request.Context(), lu)
		if err != nil {
			WriteProblem(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
			return
		}
		if !rec.Enabled {
			SetAuditMeta(c, "login", rec.Username, "disabled")
			writeAuthErr(c, http.StatusUnauthorized, "ACCOUNT_DISABLED", "账号已停用")
			return
		}
	} else if !ok || !rec.Enabled || !CheckPassword(rec.PasswordHash, body.Password) {
		SetAuditMeta(c, "login", body.Username, "failed")
		writeAuthErr(c, http.StatusUnauthorized, "BAD_CREDENTIALS", "账号或口令错误")
		return
	}
	if rec.TOTPEnabled {
		if body.TotpCode == nil || *body.TotpCode == "" {
			SetAuditMeta(c, "login", rec.Username, "totp_required")
			writeAuthErr(c, http.StatusUnauthorized, "TOTP_REQUIRED", "请输入动态口令或恢复码")
			return
		}
		code := strings.TrimSpace(*body.TotpCode)
		if !ValidateTOTP(rec.TOTPSecret, code, time.Now()) {
			// 恢复码（M6-007A）：仅在动态口令不匹配时尝试，命中后立即消耗。
			remaining, ok := ConsumeRecoveryCode(rec.TOTPRecovery, code)
			if !ok {
				SetAuditMeta(c, "login", rec.Username, "totp_failed")
				writeAuthErr(c, http.StatusUnauthorized, "TOTP_INVALID", "动态口令或恢复码错误")
				return
			}
			if _, uerr := h.users.Update(c.Request.Context(), rec.ID, UserUpdateInput{TOTPRecovery: &remaining}); uerr != nil {
				WriteProblem(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", uerr.Error())
				return
			}
			rec.TOTPRecovery = remaining
			SetAuditMeta(c, "login", rec.Username, "recovery_code")
		}
	}
	SetAuditMeta(c, "login", rec.Username, "success")
	access, refresh := issueAuthPair(rec)
	c.JSON(http.StatusOK, apigen.AuthLoginResult{
		AccessToken: access, RefreshToken: refresh,
		MustChangePassword: rec.MustChangePassword && h.forcePasswordChange(c.Request.Context()),
	})
}

func randomShadowPassword() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// syncLDAPUser 将目录用户同步为本地 shadow 用户（无本地口令登录能力）。
// 首次登录创建；角色/显示名变化时更新并吊销旧会话。
func (h *AuthHandler) syncLDAPUser(ctx context.Context, u LDAPUser) (UserRecord, error) {
	rec, ok, err := h.users.GetByUsername(ctx, u.Username)
	if err != nil {
		return UserRecord{}, err
	}
	if !ok {
		pw, err := randomShadowPassword()
		if err != nil {
			return UserRecord{}, err
		}
		if _, err := h.users.Create(ctx, UserCreateInput{
			Username: u.Username, DisplayName: u.DisplayName, Password: pw,
			Roles: u.Roles, AuthSource: "ldap",
		}); err != nil {
			return UserRecord{}, err
		}
		rec, ok, err = h.users.GetByUsername(ctx, u.Username)
		if err != nil || !ok {
			return UserRecord{}, err
		}
		return rec, nil
	}
	changed := rec.DisplayName != u.DisplayName || !sameRoles(rec.Roles, u.Roles)
	if changed {
		dn := u.DisplayName
		roles := u.Roles
		if _, err := h.users.Update(ctx, rec.ID, UserUpdateInput{DisplayName: &dn, Roles: &roles}); err != nil {
			return UserRecord{}, err
		}
		if !sameRoles(rec.Roles, u.Roles) {
			if err := h.users.BumpTokenVersion(ctx, rec.ID); err != nil {
				return UserRecord{}, err
			}
		}
		rec, _, err = h.users.GetByUsername(ctx, u.Username)
		if err != nil {
			return UserRecord{}, err
		}
	}
	return rec, nil
}

func sameRoles(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, r := range a {
		seen[r]++
	}
	for _, r := range b {
		seen[r]--
		if seen[r] < 0 {
			return false
		}
	}
	return true
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

// SetupTotp POST /auth/2fa/setup（M6-007）：生成待确认密钥。
func (h *AuthHandler) SetupTotp(c *gin.Context) {
	claims, err := ParseJWT(bearerOf(c.GetHeader("Authorization")))
	if err != nil {
		writeAuthErr(c, http.StatusUnauthorized, "TOKEN_INVALID", err.Error())
		return
	}
	rec, ok, err := h.users.GetByID(c.Request.Context(), claims.UID)
	if err != nil || !ok {
		writeAuthErr(c, http.StatusUnauthorized, "TOKEN_REVOKED", "账号不可用")
		return
	}
	secret, err := GenerateTOTPSecret()
	if err != nil {
		WriteProblem(c, 500, "https://ipam.local/problems/internal", "TOTP_SETUP_FAILED", err.Error())
		return
	}
	if _, err := h.users.Update(c.Request.Context(), rec.ID, UserUpdateInput{
		TOTPSecret: &secret, TOTPEnabled: boolPtr(false),
	}); err != nil {
		WriteProblem(c, 500, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.JSON(http.StatusOK, apigen.TotpSetupResult{
		Secret: secret, OtpauthUrl: TOTPAuthURL("IPAM", rec.Username, secret),
	})
}

// EnableTotp POST /auth/2fa/enable：校验动态口令后启用。
func (h *AuthHandler) EnableTotp(c *gin.Context) {
	claims, err := ParseJWT(bearerOf(c.GetHeader("Authorization")))
	if err != nil {
		writeAuthErr(c, http.StatusUnauthorized, "TOKEN_INVALID", err.Error())
		return
	}
	rec, ok, err := h.users.GetByID(c.Request.Context(), claims.UID)
	if err != nil || !ok {
		writeAuthErr(c, http.StatusUnauthorized, "TOKEN_REVOKED", "账号不可用")
		return
	}
	var body apigen.TotpEnableRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, "BAD_REQUEST", err.Error())
		return
	}
	if rec.TOTPSecret == "" || !ValidateTOTP(rec.TOTPSecret, body.Code, time.Now()) {
		badRequest(c, "TOTP_INVALID", "动态口令错误或未生成密钥")
		return
	}
	if _, err := h.users.Update(c.Request.Context(), rec.ID, UserUpdateInput{TOTPEnabled: boolPtr(true)}); err != nil {
		WriteProblem(c, 500, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	SetAuditMeta(c, "totp_enable", rec.Username, "success")
	access, refresh := issueAuthPair(rec)
	c.JSON(http.StatusOK, apigen.AuthLoginResult{AccessToken: access, RefreshToken: refresh, MustChangePassword: false})
}

// DisableTotp POST /auth/2fa/disable：校验当前口令后关闭。
func (h *AuthHandler) DisableTotp(c *gin.Context) {
	claims, err := ParseJWT(bearerOf(c.GetHeader("Authorization")))
	if err != nil {
		writeAuthErr(c, http.StatusUnauthorized, "TOKEN_INVALID", err.Error())
		return
	}
	rec, ok, err := h.users.GetByID(c.Request.Context(), claims.UID)
	if err != nil || !ok {
		writeAuthErr(c, http.StatusUnauthorized, "TOKEN_REVOKED", "账号不可用")
		return
	}
	var body apigen.TotpDisableRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, "BAD_REQUEST", err.Error())
		return
	}
	if !CheckPassword(rec.PasswordHash, body.Password) {
		writeAuthErr(c, http.StatusUnauthorized, "BAD_CREDENTIALS", "当前口令不正确")
		return
	}
	empty := ""
	noCodes := []string{}
	if _, err := h.users.Update(c.Request.Context(), rec.ID, UserUpdateInput{
		TOTPSecret: &empty, TOTPEnabled: boolPtr(false), TOTPRecovery: &noCodes,
	}); err != nil {
		WriteProblem(c, 500, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	SetAuditMeta(c, "totp_disable", rec.Username, "success")
	c.Status(http.StatusNoContent)
}

// LogoutAll POST /auth/logout-all：token_version+1，注销全部会话。
func (h *AuthHandler) LogoutAll(c *gin.Context) {
	claims, err := ParseJWT(bearerOf(c.GetHeader("Authorization")))
	if err != nil {
		writeAuthErr(c, http.StatusUnauthorized, "TOKEN_INVALID", err.Error())
		return
	}
	if err := h.users.BumpTokenVersion(c.Request.Context(), claims.UID); err != nil {
		WriteProblem(c, 500, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	SetAuditMeta(c, "logout_all", claims.Sub, "success")
	c.Status(http.StatusNoContent)
}

// ChangeAuthPassword POST /auth/password（M6-001：自助改密）
// 校验旧口令 → 口令策略 → 更新散列 → 清除 must-change → token_version+1 → 颁发新令牌对。
func (h *AuthHandler) ChangeAuthPassword(c *gin.Context) {
	claims, err := ParseJWT(bearerOf(c.GetHeader("Authorization")))
	if err != nil {
		writeAuthErr(c, http.StatusUnauthorized, "TOKEN_INVALID", err.Error())
		return
	}
	rec, ok, err := h.users.GetByID(c.Request.Context(), claims.UID)
	if err != nil {
		WriteProblem(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	if !ok || !rec.Enabled {
		writeAuthErr(c, http.StatusUnauthorized, "TOKEN_REVOKED", "账号不可用")
		return
	}
	var body apigen.AuthChangePasswordRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, "BAD_REQUEST", err.Error())
		return
	}
	if rec.AuthSource == "ldap" {
		badRequest(c, "DIRECTORY_ACCOUNT", "目录账号请在 LDAP/AD 中修改口令")
		return
	}
	if !CheckPassword(rec.PasswordHash, body.OldPassword) {
		SetAuditMeta(c, "password_change", rec.Username, "old_password_mismatch")
		writeAuthErr(c, http.StatusUnauthorized, "BAD_CREDENTIALS", "当前口令不正确")
		return
	}
	if body.NewPassword == body.OldPassword {
		badRequest(c, "SAME_PASSWORD", "新口令不能与当前口令相同")
		return
	}
	if err := ValidatePassword(rec.Username, body.NewPassword); err != nil {
		badRequest(c, "WEAK_PASSWORD", err.Error())
		return
	}
	clearMustChange := false
	if _, err := h.users.Update(c.Request.Context(), rec.ID, UserUpdateInput{
		Password: &body.NewPassword, MustChangePassword: &clearMustChange,
	}); err != nil {
		WriteProblem(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	// 会话版本 +1：其他设备/旧令牌立即失效；当前请求用新版本重新签发令牌对。
	if err := h.users.BumpTokenVersion(c.Request.Context(), rec.ID); err != nil {
		WriteProblem(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	fresh, ok, err := h.users.GetByID(c.Request.Context(), rec.ID)
	if err != nil || !ok {
		WriteProblem(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", "读取更新后的账号失败")
		return
	}
	SetAuditMeta(c, "password_change", rec.Username, "success")
	access, refresh := issueAuthPair(fresh)
	c.JSON(http.StatusOK, apigen.AuthLoginResult{
		AccessToken: access, RefreshToken: refresh,
		MustChangePassword: false,
	})
}

// GetTotpRecoveryStatus GET /auth/2fa/recovery-codes（M6-007A）。
func (h *AuthHandler) GetTotpRecoveryStatus(c *gin.Context) {
	rec, ok := h.currentUser(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, apigen.TotpRecoveryStatus{Remaining: len(rec.TOTPRecovery)})
}

// GenerateTotpRecoveryCodes POST /auth/2fa/recovery-codes：校验口令或动态口令后重置恢复码。
func (h *AuthHandler) GenerateTotpRecoveryCodes(c *gin.Context) {
	rec, ok := h.currentUser(c)
	if !ok {
		return
	}
	if !rec.TOTPEnabled {
		badRequest(c, "TOTP_NOT_ENABLED", "请先启用双因素认证再生成恢复码")
		return
	}
	var body apigen.TotpRecoveryGenerateRequest
	_ = c.ShouldBindJSON(&body)
	passwordOK := body.Password != nil && *body.Password != "" && rec.AuthSource != "ldap" &&
		CheckPassword(rec.PasswordHash, *body.Password)
	totpOK := body.TotpCode != nil && *body.TotpCode != "" &&
		ValidateTOTP(rec.TOTPSecret, *body.TotpCode, time.Now())
	if !passwordOK && !totpOK {
		SetAuditMeta(c, "totp_recovery_generate", rec.Username, "auth_failed")
		writeAuthErr(c, http.StatusUnauthorized, "BAD_CREDENTIALS", "当前口令或动态口令不正确")
		return
	}
	codes, hashes, err := GenerateRecoveryCodes(recoveryCodeCount)
	if err != nil {
		WriteProblem(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "TOTP_RECOVERY_FAILED", err.Error())
		return
	}
	if _, err := h.users.Update(c.Request.Context(), rec.ID, UserUpdateInput{TOTPRecovery: &hashes}); err != nil {
		WriteProblem(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	SetAuditMeta(c, "totp_recovery_generate", rec.Username, "success")
	c.JSON(http.StatusOK, apigen.TotpRecoveryCodes{Codes: codes, Remaining: len(codes)})
}

// currentUser 解析 Bearer 并加载当前账号（会话版本校验收口）。
func (h *AuthHandler) currentUser(c *gin.Context) (UserRecord, bool) {
	claims, err := ParseJWT(bearerOf(c.GetHeader("Authorization")))
	if err != nil {
		writeAuthErr(c, http.StatusUnauthorized, "TOKEN_INVALID", err.Error())
		return UserRecord{}, false
	}
	rec, ok, err := h.users.GetByID(c.Request.Context(), claims.UID)
	if err != nil || !ok || !rec.Enabled || (claims.Ver != 0 && claims.Ver != rec.TokenVersion) {
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
	mustChange := false
	totpEnabled := false
	if rec, ok, lerr := h.users.GetByID(c.Request.Context(), claims.UID); lerr == nil && ok {
		totpEnabled = rec.TOTPEnabled
		mustChange = rec.MustChangePassword && h.forcePasswordChange(c.Request.Context())
		if rec.DisplayName != "" {
			realName = rec.DisplayName
		}
	}
	// M2-035：权限点集合（hasPerm 逐点过滤，含自定义角色 permLookup）
	perms := make([]string, 0, len(allPermList))
	for _, p := range allPermList {
		if hasPerm(claims, p, h.permLookup, c.Request.Context()) {
			perms = append(perms, p)
		}
	}
	c.JSON(http.StatusOK, apigen.UserInfo{
		UserId:             claims.UID,
		Username:           claims.Sub,
		RealName:           realName,
		Roles:              claims.Roles,
		Permissions:        &perms,
		Desc:               strPtr("IPAM 控制面账号"),
		HomePath:           "/overview",
		MustChangePassword: mustChange,
		TotpEnabled:        &totpEnabled,
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
			SetAuditMeta(c, "logout", claims.Sub, "success")
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

func boolPtr(b bool) *bool { return &b }

func writeAuthErr(c *gin.Context, status int, code, detail string) {
	WriteProblem(c, status, "https://ipam.local/problems/unauthorized", code, detail)
}

// GetAuthUserInfoAlias GET /user/info（vben 底座约定路径，与 GetAuthUserInfo 同一资源）
func (h *AuthHandler) GetAuthUserInfoAlias(c *gin.Context) {
	h.GetAuthUserInfo(c)
}
