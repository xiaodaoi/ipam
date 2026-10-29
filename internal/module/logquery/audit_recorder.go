package logquery

import (
	"log"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/xiaodaoi/ipam/internal/pkg/auditmeta"
)

// ActorProvider 从请求提取调用者身份（§12.3）。
// M5 JWT 接入前默认 system/anonymous；接入后由控制面注入 token 解析实现。
type ActorProvider = func(c *gin.Context) (string, string, string)

func DefaultActorProvider(*gin.Context) (ActorType, string, string) {
	return ActorSystem, "control-plane", ""
}

// NewAuditRecorder 变更类请求（POST/PATCH/DELETE）审计中间件。
// resource 取路由模板（路径参数归一化），status 为响应码；匿名路由（FullPath 空）不入库。
func NewAuditRecorder(store AuditStore, provider ...ActorProvider) gin.HandlerFunc {
	extract := DefaultActorProvider
	if len(provider) > 0 && provider[0] != nil {
		raw := provider[0]
		extract = func(c *gin.Context) (ActorType, string, string) {
			at, actor, sub := raw(c)
			return ActorType(at), actor, sub
		}
	}
	return func(c *gin.Context) {
		c.Next()

		switch c.Request.Method {
		case "POST", "PATCH", "DELETE":
		default:
			return
		}
		if store == nil || c.FullPath() == "" {
			return
		}
		atype, actor, tokenSub := extract(c)
		resource := strings.TrimPrefix(c.FullPath(), "/api/v1")
		action := ActionForMethod(c.Request.Method)
		if v := c.GetString(auditmeta.ActionKey); v != "" {
			action = v
		}
		if v := c.GetString(auditmeta.ActorKey); v != "" {
			actor = v
		}
		detail := c.GetString(auditmeta.DetailKey)
		if _, err := store.Append(c.Request.Context(), AuditEntry{
			ActorType: atype, Actor: actor, TokenSub: tokenSub,
			Method:   c.Request.Method,
			Path:     c.Request.URL.Path,
			Action:   action,
			Resource: resource,
			Status:   c.Writer.Status(),
			Detail:   detail,
		}); err != nil {
			log.Printf("[audit] append %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
		}
	}
}
