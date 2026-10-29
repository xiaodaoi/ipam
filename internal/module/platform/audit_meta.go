package platform

import (
	"github.com/gin-gonic/gin"

	"github.com/xiaodaoi/ipam/internal/pkg/auditmeta"
)

// 审计上下文键（M6-001）：认证类请求不经过业务 handler 的成功路径，
// 由 AuthHandler 显式写入动作/身份，审计中间件统一落库，避免匿名 create 噪音。
const (
	AuditActionKey = auditmeta.ActionKey
	AuditActorKey  = auditmeta.ActorKey
	AuditDetailKey = auditmeta.DetailKey
)

// SetAuditMeta 标记本次请求的审计语义（动作/身份/结果说明）。
func SetAuditMeta(c *gin.Context, action, actor, detail string) {
	if action != "" {
		c.Set(AuditActionKey, action)
	}
	if actor != "" {
		c.Set(AuditActorKey, actor)
	}
	if detail != "" {
		c.Set(AuditDetailKey, detail)
	}
}
