package platform

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// RBAC 写权限拦截（M5-003，§12.3/§13.2）。
//
// 规则：
//   - 白名单路径（登录/探活）与 GET/HEAD/OPTIONS 一律放行；
//   - 变更类请求（POST/PATCH/DELETE）须携带有效 JWT；
//     user/system 角色写操作 403，admin 放行；
//   - 无令牌 401（区别于 403，供前端引导重新登录）。
//
// 位置：审计中间件之前（被 403 的请求不入审计——拦截行为本身即审计语义，
// 如需审计拦截事件可在 M5 后续卡扩展）。
func NewRBACMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodPost, http.MethodPatch, http.MethodDelete:
		default:
			c.Next()
			return
		}

		// 登录端点自身放行（无令牌前提）
		if c.FullPath() == "/api/v1/auth/login" {
			c.Next()
			return
		}

		header := c.GetHeader("Authorization")
		claims, err := ParseJWT(strings.TrimPrefix(header, "Bearer "))
		if err != nil {
			problem.Write(c, http.StatusUnauthorized,
				"https://ipam.local/problems/unauthorized", "TOKEN_MISSING", "未携带有效访问令牌")
			c.Abort()
			return
		}
		if !HasRole(claims, "admin") {
			problem.Write(c, http.StatusForbidden,
				"https://ipam.local/problems/forbidden", "FORBIDDEN",
				"当前角色无写权限（需 admin）")
			c.Abort()
			return
		}
		c.Next()
	}
}
