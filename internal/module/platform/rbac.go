package platform

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// RBAC 认证+授权拦截（M5-003/M5-010，§12.3/§13.2）。
//
// 规则：
//   - 登录/登出端点放行（无令牌前提）；
//   - 其余请求须携带有效 JWT，且用户存在、未停用、会话版本匹配
//     （M5-010：改密/停用/角色变更 → token_version+1，存量令牌立即失效）；
//   - 变更类请求（POST/PATCH/DELETE）另须 admin 角色（user 只读）。
//
// 位置：审计中间件之前（被拦截的请求不入审计）。
func NewRBACMiddleware(users UserStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 登录/登出端点自身放行（无令牌前提）
		if fp := c.FullPath(); fp == "/api/v1/auth/login" || fp == "/api/v1/auth/logout" {
			c.Next()
			return
		}
		// SPA 静态资源与未知路由（FullPath 为空）放行——认证仅覆盖 /api/v1 业务端点
		if c.FullPath() == "" {
			c.Next()
			return
		}

		claims, err := ParseJWT(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
		if err != nil {
			problem.Write(c, http.StatusUnauthorized,
				"https://ipam.local/problems/unauthorized", "TOKEN_MISSING", "未携带有效访问令牌")
			c.Abort()
			return
		}

		// M5-010：用户状态/会话版本校验（禁用/吊销立即失效；GET 同样受限）
		user, ok, err := users.GetByUsername(c.Request.Context(), claims.Sub)
		if err != nil {
			problem.Write(c, http.StatusInternalServerError,
				"https://ipam.local/problems/internal", "DB_ERROR", err.Error())
			c.Abort()
			return
		}
		if !ok || !user.Enabled || (claims.Ver != 0 && claims.Ver != user.TokenVersion) {
			problem.Write(c, http.StatusUnauthorized,
				"https://ipam.local/problems/unauthorized", "TOKEN_REVOKED", "令牌已吊销或账号不可用")
			c.Abort()
			return
		}

		// 写操作 admin 检查（M5-003 现状）
		switch c.Request.Method {
		case http.MethodPost, http.MethodPatch, http.MethodDelete:
			if !HasRole(claims, "admin") {
				problem.Write(c, http.StatusForbidden,
					"https://ipam.local/problems/forbidden", "FORBIDDEN",
					"当前角色无写权限（需 admin）")
				c.Abort()
				return
			}
		}
		c.Next()
	}
}
