package logquery

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// AuditHandler 实现 apigen.ServerInterface 中 /audits 端点（M4-003，FR-F）。
type AuditHandler struct {
	store AuditStore
}

func NewAuditHandler(store AuditStore) *AuditHandler { return &AuditHandler{store: store} }

// ListOperationAudits GET /audits
func (h *AuditHandler) ListOperationAudits(c *gin.Context, params apigen.ListOperationAuditsParams) {
	f := AuditFilter{
		From:     params.From,
		Cursor:   derefStr(params.Cursor),
		PageSize: derefInt(params.PageSize, DefaultPage),
	}
	if params.To != nil {
		f.To = *params.To
	}
	if params.ActorType != nil {
		f.ActorType = ActorType(string(*params.ActorType))
	}
	if params.Action != nil {
		f.Action = *params.Action
	}
	if params.Q != nil {
		f.Q = *params.Q
	}
	if !f.From.IsZero() && !f.To.IsZero() && f.To.Sub(f.From) > MaxWindow {
		problem.Write(c, http.StatusBadRequest,
			"https://ipam.local/problems/bad-request", "INVALID_FILTER", "时间窗超过 31 天上限")
		return
	}
	page, err := h.store.Query(c.Request.Context(), f)
	if err != nil {
		problem.Write(c, http.StatusServiceUnavailable,
			"https://ipam.local/problems/audit-store-down", "AUDIT_DOWN", "审计存储暂不可达")
		return
	}
	items := make([]apigen.AuditEntry, 0, len(page.Items))
	for _, e := range page.Items {
		items = append(items, toGenAudit(e))
	}
	var next *string
	if page.NextCursor != "" {
		next = &page.NextCursor
	}
	c.JSON(http.StatusOK, apigen.AuditPage{Items: items, NextCursor: next, Total: &page.Total})
}

func toGenAudit(e AuditEntry) apigen.AuditEntry {
	return apigen.AuditEntry{
		Id:        int(e.ID),
		Ts:        e.TS,
		ActorType: apigen.AuditEntryActorType(e.ActorType),
		Actor:     optStr(e.Actor),
		TokenSub:  optStr(e.TokenSub),
		Method:    e.Method,
		Path:      e.Path,
		Action:    e.Action,
		Resource:  e.Resource,
		Status:    e.Status,
		Detail:    optStr(e.Detail),
	}
}

// ActionForMethod 变更方法 → 归一动作（特殊端点由调用方在 detail 补充语义）。
func ActionForMethod(method string) string {
	switch method {
	case http.MethodPost:
		return "create"
	case http.MethodPatch:
		return "update"
	case http.MethodDelete:
		return "delete"
	default:
		return strings.ToLower(method)
	}
}
