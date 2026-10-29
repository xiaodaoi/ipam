// Package auditmeta 定义审计上下文键（M6-001）。
// 放在中立包中，避免 platform 与 logquery 之间产生导入环。
package auditmeta

const (
	ActionKey = "ipam.audit.action"
	ActorKey  = "ipam.audit.actor"
	DetailKey = "ipam.audit.detail"
)
