package dns

import (
	"net/http"

	guuid "github.com/google/uuid"
	rtypes "github.com/oapi-codegen/runtime/types"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// ForwardHandler 实现 apigen.ServerInterface 中转发规则端点。
type ForwardHandler struct {
	svc *ForwardService
}

func NewForwardHandler(svc *ForwardService) *ForwardHandler { return &ForwardHandler{svc: svc} }

// ListForwardRules GET /forward-rules
func (h *ForwardHandler) ListForwardRules(c *gin.Context) {
	list, err := h.svc.repo.List(c.Request.Context())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	items := make([]apigen.ForwardRule, 0, len(list))
	for _, r := range list {
		items = append(items, toGenRule(r))
	}
	c.JSON(http.StatusOK, apigen.ForwardRuleList{Items: items, Total: &[]int{len(items)}[0]})
}

// CreateForwardRule POST /forward-rules
func (h *ForwardHandler) CreateForwardRule(c *gin.Context) {
	var body apigen.ForwardRuleCreate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	ids := make([]string, 0, len(body.UpstreamIds))
	for _, id := range body.UpstreamIds {
		ids = append(ids, id.String())
	}
	dryRun := body.DryRun != nil && *body.DryRun
	r := ForwardRule{Domain: body.Domain, UpstreamIDs: ids, Enabled: deref(body.Enabled, true), Note: derefStr(body.Note)}
	saved, cmds, err := h.svc.Create(c.Request.Context(), r, dryRun)
	if err != nil {
		if err == ErrUpstreamNotFound {
			problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "UPSTREAM_NOT_FOUND", "上游不存在")
			return
		}
		if err == ErrUnboundDown {
			problem.Write(c, http.StatusServiceUnavailable, "https://ipam.local/problems/unbound-down", "UNBOUND_DOWN", "已保存但 unbound 下发失败")
			return
		}
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "INTERNAL", err.Error())
		return
	}
	if dryRun {
		c.JSON(http.StatusOK, apigen.ForwardRuleDryRun{Commands: cmds})
		return
	}
	c.JSON(http.StatusCreated, toGenRule(saved))
}

// UpdateForwardRule PATCH /forward-rules/{ruleId}
func (h *ForwardHandler) UpdateForwardRule(c *gin.Context, ruleId rtypes.UUID) {
	var body struct {
		UpstreamIds *[]rtypes.UUID `json:"upstreamIds"`
		Enabled     *bool          `json:"enabled"`
		Note        *string        `json:"note"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	id := ruleId.String()
	cur := ForwardRule{ID: id, Domain: "unused"}
	// 加载现有一并更新（repo.List 简易读取）
	list, _ := h.svc.repo.List(c.Request.Context())
	var found bool
	for _, r := range list {
		if r.ID == id {
			cur = r
			found = true
		}
	}
	if !found {
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "FORWARD_RULE_NOT_FOUND", "规则不存在")
		return
	}
	if body.UpstreamIds != nil {
		ids := make([]string, 0, len(*body.UpstreamIds))
		for _, u := range *body.UpstreamIds {
			ids = append(ids, u.String())
		}
		cur.UpstreamIDs = ids
	}
	if body.Enabled != nil {
		cur.Enabled = *body.Enabled
	}
	if body.Note != nil {
		cur.Note = *body.Note
	}
	if err := h.svc.repo.Update(c.Request.Context(), cur); err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	_ = h.svc.sync(c.Request.Context())
	c.JSON(http.StatusOK, toGenRule(cur))
}

// DeleteForwardRule DELETE /forward-rules/{ruleId}
func (h *ForwardHandler) DeleteForwardRule(c *gin.Context, ruleId rtypes.UUID) {
	if err := h.svc.repo.Delete(c.Request.Context(), ruleId.String()); err != nil {
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "FORWARD_RULE_NOT_FOUND", "规则不存在")
		return
	}
	_ = h.svc.sync(c.Request.Context())
	c.Status(http.StatusNoContent)
}

func toGenRule(r ForwardRule) apigen.ForwardRule {
	ids := make([]rtypes.UUID, 0, len(r.UpstreamIDs))
	for _, id := range r.UpstreamIDs {
		u := guuid.MustParse(id)
		ids = append(ids, rtypes.UUID(u))
	}
	return apigen.ForwardRule{
		Id:          rtypes.UUID(guuid.MustParse(r.ID)),
		Domain:      r.Domain,
		UpstreamIds: ids,
		Enabled:     r.Enabled,
		Note:        strP(r.Note),
	}
}

func strP(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
