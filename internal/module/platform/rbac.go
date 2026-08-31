package platform

import (
	"context"
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
// ── 权限点模型（M2-035）：域 × 读写；内置角色硬编码映射，自定义角色经 permLookup 查库 ──

// domainOf 路径 → 权限域（/api/v1 前缀已含；空=放行）。
func domainOf(fullPath string) string {
	switch {
	case strings.HasPrefix(fullPath, "/api/v1/system/info"):
		return "dash"
	case strings.HasPrefix(fullPath, "/api/v1/dashboard"):
		return "dash"
	case strings.HasPrefix(fullPath, "/api/v1/logs"), strings.HasPrefix(fullPath, "/api/v1/audits"):
		return "logs"
	case strings.HasPrefix(fullPath, "/api/v1/subnets"), strings.HasPrefix(fullPath, "/api/v1/dhcp"),
		strings.HasPrefix(fullPath, "/api/v1/reservations"), strings.HasPrefix(fullPath, "/api/v1/ledger"),
		strings.HasPrefix(fullPath, "/api/v1/assets"), strings.HasPrefix(fullPath, "/api/v1/dualstack"):
		return "dhcp"
	case strings.HasPrefix(fullPath, "/api/v1/upstreams"), strings.HasPrefix(fullPath, "/api/v1/forward-rules"),
		strings.HasPrefix(fullPath, "/api/v1/dns"):
		return "dns"
	case strings.HasPrefix(fullPath, "/api/v1/users"), strings.HasPrefix(fullPath, "/api/v1/roles"),
		strings.HasPrefix(fullPath, "/api/v1/orgs"):
		return "system"
	}
	return ""
}

func permSet(vs ...string) map[string]bool {
	m := map[string]bool{}
	for _, v := range vs {
		m[v] = true
	}
	return m
}

var allPermList = []string{"dash:read", "dash:write", "logs:read", "logs:write", "dhcp:read", "dhcp:write", "dns:read", "dns:write", "system:read", "system:write", "assets:read", "assets:write"}

// builtinRolePerms 内置角色 → 权限集（admin 全量；operator 业务读写；auditor/user 只读）。
var builtinRolePerms = map[string]map[string]bool{
	"admin":    permSet(allPermList...),
	"operator": permSet("dash:read", "logs:read", "dhcp:read", "dhcp:write", "dns:read", "dns:write", "assets:read", "assets:write", "system:read"),
	"auditor":  permSet("dash:read", "logs:read", "dhcp:read", "dns:read", "assets:read", "system:read"),
	"user":     permSet("dash:read", "logs:read", "dhcp:read", "dns:read", "assets:read"),
}

// hasPerm claims.Roles 逐角色解析：内置角色映射优先，非内置经 permLookup 查 roles 表。
func hasPerm(claims JWTClaims, need string, lookup func(ctx context.Context, role string) ([]string, bool), ctx context.Context) bool {
	for _, role := range claims.Roles {
		if ps, ok := builtinRolePerms[role]; ok {
			if ps[need] {
				return true
			}
			continue
		}
		if lookup != nil {
			if perms, ok := lookup(ctx, role); ok {
				for _, p := range perms {
					if p == need {
						return true
					}
				}
			}
		}
	}
	return false
}

func NewRBACMiddleware(users UserStore, bl *TokenBlacklist, permLookup func(ctx context.Context, role string) ([]string, bool)) gin.HandlerFunc {
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

		tok := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		claims, err := ParseJWT(tok)
		if err != nil {
			problem.Write(c, http.StatusUnauthorized,
				"https://ipam.local/problems/unauthorized", "TOKEN_MISSING", "未携带有效访问令牌")
			c.Abort()
			return
		}
		// M5-011：登出黑名单（已吊销令牌立即失效）
		if bl != nil && bl.Revoked(TokenHash(tok)) {
			problem.Write(c, http.StatusUnauthorized,
				"https://ipam.local/problems/unauthorized", "TOKEN_REVOKED", "令牌已吊销（已登出）")
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

		// M2-035：域权限拦截（读 → <域>:read；写 → <域>:write）
		if dom := domainOf(c.FullPath()); dom != "" {
			need := dom + ":read"
			if c.Request.Method == http.MethodPost || c.Request.Method == http.MethodPatch || c.Request.Method == http.MethodDelete {
				need = dom + ":write"
			}
			if !hasPerm(claims, need, permLookup, c.Request.Context()) {
				problem.Write(c, http.StatusForbidden,
					"https://ipam.local/problems/forbidden", "FORBIDDEN",
					"当前角色无该权限（需要 "+need+"）")
				c.Abort()
				return
			}
		}
		c.Next()
	}
}
