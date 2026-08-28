package dualstack

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	guuid "github.com/google/uuid"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// Handler 实现 apigen.ServerInterface 中 dualstack 域端点（M2-012）。
type Handler struct{ store Store }

func NewHandler(store Store) *Handler { return &Handler{store: store} }

// ListDualstackTemplates GET /dualstack/templates
func (h *Handler) ListDualstackTemplates(c *gin.Context) {
	items, err := h.store.List(c.Request.Context())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError,
			"https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	gen := make([]apigen.DualstackTemplate, 0, len(items))
	for _, t := range items {
		gen = append(gen, toGenTemplate(t))
	}
	c.JSON(http.StatusOK, apigen.DualstackTemplateList{Items: gen})
}

// CreateDualstackTemplate POST /dualstack/templates
func (h *Handler) CreateDualstackTemplate(c *gin.Context) {
	var body apigen.DualstackTemplateCreate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest,
			"https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	// CIDR 基础校验（PG cidr 列兜底前的前置友好错误）
	if !strings.Contains(body.Ipv4Cidr, "/") || !strings.Contains(body.Ipv6Prefix, "/") {
		problem.Write(c, http.StatusBadRequest,
			"https://ipam.local/problems/bad-request", "INVALID_CIDR",
			"ipv4Cidr 与 ipv6Prefix 须为 CIDR 形态（如 192.168.0.0/24、2407::/64）")
		return
	}
	created, err := h.store.Create(c.Request.Context(), fromGenCreate(body))
	if err != nil {
		problem.Write(c, http.StatusInternalServerError,
			"https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.JSON(http.StatusCreated, toGenTemplate(created))
}

// DeleteDualstackTemplate DELETE /dualstack/templates/{templateId}
func (h *Handler) DeleteDualstackTemplate(c *gin.Context, templateId guuid.UUID) {
	if err := h.store.Delete(c.Request.Context(), templateId.String()); err != nil {
		problem.Write(c, http.StatusInternalServerError,
			"https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}

func toGenTemplate(t Template) apigen.DualstackTemplate {
	id := guuid.MustParse(t.ID)
	dnsSync := t.DnsSync
	grace := t.GraceHours
	return apigen.DualstackTemplate{
		Id:         id,
		Name:       t.Name,
		Ipv4Cidr:   t.V4Cidr,
		Ipv6Prefix: t.V6Prefix,
		Encoding:   apigen.DualstackTemplateEncoding(t.Encoding),
		Expr:       t.Expr,
		DnsSync:    &dnsSync,
		GraceHours: &grace,
		Enabled:    t.Enabled,
	}
}

func fromGenCreate(b apigen.DualstackTemplateCreate) Template {
	grace := 24
	if b.GraceHours != nil {
		grace = *b.GraceHours
	}
	dnsSync := true
	if b.DnsSync != nil {
		dnsSync = *b.DnsSync
	}
	enabled := true
	if b.Enabled != nil {
		enabled = *b.Enabled
	}
	return Template{
		Name: b.Name, V4Cidr: b.Ipv4Cidr, V6Prefix: b.Ipv6Prefix,
		Encoding: string(b.Encoding), Expr: b.Expr,
		DnsSync: dnsSync, GraceHours: grace, Enabled: enabled,
	}
}
