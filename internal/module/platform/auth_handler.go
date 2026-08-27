package platform

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
)

// AuthHandler 实现 apigen.ServerInterface 中 auth 域端点（M5-001 PoC 直通）。
type AuthHandler struct{}

func NewAuthHandler() *AuthHandler { return &AuthHandler{} }

func checkCredentials(username, password string) bool {
	return username == pocUsername && password == pocPassword()
}

// AuthLogin POST /auth/login
func (h *AuthHandler) AuthLogin(c *gin.Context) {
	var body apigen.AuthLoginRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		writeAuthErr(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	if !checkCredentials(body.Username, body.Password) {
		writeAuthErr(c, http.StatusUnauthorized, "BAD_CREDENTIALS", "账号或口令错误")
		return
	}
	c.JSON(http.StatusOK, apigen.AuthLoginResult{AccessToken: IssueToken(timeNowUTC())})
}

// GetAuthUserInfo GET /auth/user/info
func (h *AuthHandler) GetAuthUserInfo(c *gin.Context) {
	token := bearerOf(c.GetHeader("Authorization"))
	userID, err := ValidateToken(token)
	if err != nil {
		writeAuthErr(c, http.StatusUnauthorized, "TOKEN_INVALID", err.Error())
		return
	}
	name := pocUsername
	if userID == "" {
		name = "user-" + userID
	}
	c.JSON(http.StatusOK, apigen.UserInfo{
		UserId:   userID,
		Username: name,
		RealName: "管理员",
		Roles:    []string{"admin"},
		Desc:     strPtr("IPAM 控制面管理员"),
		HomePath: "/overview",
	})
}

// ListAuthCodes GET /auth/codes（PoC 空=按钮级权限不启用）
func (h *AuthHandler) ListAuthCodes(c *gin.Context) {
	if _, err := ValidateToken(bearerOf(c.GetHeader("Authorization"))); err != nil {
		writeAuthErr(c, http.StatusUnauthorized, "TOKEN_INVALID", err.Error())
		return
	}
	c.JSON(http.StatusOK, []string{})
}

// LogoutAuth POST /auth/logout（PoC 无状态令牌，仅闭环前端登出）
func (h *AuthHandler) LogoutAuth(c *gin.Context) {
	ok := true
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
