package platform

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
)

// AuthHandler 实现 apigen.ServerInterface 中 auth 域端点（M5-004/M5-011：bcrypt 校验+登出吊销）。
type AuthHandler struct {
	users UserStore
	bl    *TokenBlacklist
}

func NewAuthHandler(users UserStore, bl *TokenBlacklist) *AuthHandler {
	return &AuthHandler{users: users, bl: bl}
}

// AuthLogin POST /auth/login
func (h *AuthHandler) AuthLogin(c *gin.Context) {
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
	c.JSON(http.StatusOK, apigen.AuthLoginResult{AccessToken: IssueTokenFor(rec.Username, rec.ID, rec.Roles, rec.TokenVersion)})
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
	c.JSON(http.StatusOK, apigen.UserInfo{
		UserId:   claims.UID,
		Username: claims.Sub,
		RealName: realName,
		Roles:    claims.Roles,
		Desc:     strPtr("IPAM 控制面账号"),
		HomePath: "/overview",
	})
}

// ListAuthCodes GET /auth/codes（PoC 空=按钮级权限不启用）
func (h *AuthHandler) ListAuthCodes(c *gin.Context) {
	claims, err := ParseJWT(bearerOf(c.GetHeader("Authorization")))
	if err != nil {
		writeAuthErr(c, http.StatusUnauthorized, "TOKEN_INVALID", err.Error())
		return
	}
	c.JSON(http.StatusOK, claims.Roles)
}

// LogoutAuth POST /auth/logout（M5-011：登出即吊销——有效令牌入黑名单至自然过期）。
func (h *AuthHandler) LogoutAuth(c *gin.Context) {
	ok := true
	if tok := bearerOf(c.GetHeader("Authorization")); tok != "" {
		if claims, perr := ParseJWT(tok); perr == nil {
			h.bl.Add(TokenHash(tok), time.Unix(claims.Exp, 0))
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
