package dualstack

import (
	"errors"
	"net/http"
	"strings"
	"time"

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
		Id:          id,
		Name:        t.Name,
		Ipv4Cidr:    t.V4Cidr,
		Ipv6Prefix:  t.V6Prefix,
		Encoding:    apigen.DualstackTemplateEncoding(t.Encoding),
		Expr:        t.Expr,
		DnsSync:     &dnsSync,
		GraceHours:  &grace,
		Enabled:     t.Enabled,
		MatchScheme: schemePtr(t.MatchScheme),
	}
}

func fromGenUpdate(b apigen.DualstackTemplateUpdate, id string) Template {
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
		ID: id, Name: b.Name, V4Cidr: b.Ipv4Cidr, V6Prefix: b.Ipv6Prefix,
		Encoding: string(b.Encoding), Expr: b.Expr, DnsSync: dnsSync, GraceHours: grace, Enabled: enabled,
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

// UpdateDualstackTemplate PATCH /dualstack/templates/{templateId}（M2-028）
func (h *Handler) UpdateDualstackTemplate(c *gin.Context, templateId guuid.UUID) {
	var body apigen.DualstackTemplateUpdate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest,
			"https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	if !strings.Contains(body.Ipv4Cidr, "/") || !strings.Contains(body.Ipv6Prefix, "/") {
		problem.Write(c, http.StatusBadRequest,
			"https://ipam.local/problems/bad-request", "INVALID_CIDR",
			"ipv4Cidr 与 ipv6Prefix 须为 CIDR 形态（如 192.168.0.0/24、2407::/64）")
		return
	}
	updated, err := h.store.Update(c.Request.Context(), fromGenUpdate(body, templateId.String()))
	if err != nil {
		if errors.Is(err, ErrTemplateNotFound) {
			problem.Write(c, http.StatusNotFound,
				"https://ipam.local/problems/not-found", "TEMPLATE_NOT_FOUND", "模板不存在")
			return
		}
		problem.Write(c, http.StatusInternalServerError,
			"https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.JSON(http.StatusOK, toGenTemplate(updated))
}

// schemeVal/schemePtr 匹配方案与生成类型的互转（空回落 auto）。
func schemeVal(s *apigen.DualstackTemplateMatchScheme) string {
	if s == nil {
		return "auto"
	}
	return string(*s)
}

func schemePtr(s string) *apigen.DualstackTemplateMatchScheme {
	v := apigen.DualstackTemplateMatchScheme(normScheme(s))
	return &v
}

// normalizeIdentityKey 归一化 MAC/DUID 键（小写去冒号）。
func normalizeIdentityKey(s string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), ":", ""))
}

// ListDualstackIdentities GET /dualstack/identities（M3-013）
func (h *Handler) ListDualstackIdentities(c *gin.Context) {
	items, err := h.store.ListIdentities(c.Request.Context())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError,
			"https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	gen := make([]apigen.DualstackIdentity, 0, len(items))
	for _, it := range items {
		gen = append(gen, apigen.DualstackIdentity{
			Mac: it.Mac, Duid: it.Duid,
			Source: apigen.DualstackIdentitySource(it.Source),
			Note:   strPtr(it.Note), UpdatedAt: timePtr(it.UpdatedAt),
		})
	}
	c.JSON(http.StatusOK, apigen.DualstackIdentityList{Items: gen})
}

// CreateDualstackIdentity POST /dualstack/identities（人工确认，优先级最高）
func (h *Handler) CreateDualstackIdentity(c *gin.Context) {
	var body apigen.DualstackIdentityCreate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest,
			"https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	mac := normalizeIdentityKey(body.Mac)
	duid := normalizeIdentityKey(body.Duid)
	if mac == "" || duid == "" {
		problem.Write(c, http.StatusBadRequest,
			"https://ipam.local/problems/bad-request", "BAD_REQUEST", "mac 与 duid 不能为空")
		return
	}
	note := ""
	if body.Note != nil {
		note = *body.Note
	}
	it, err := h.store.UpsertIdentity(c.Request.Context(), mac, duid, "admin", note)
	if err != nil {
		problem.Write(c, http.StatusInternalServerError,
			"https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.JSON(http.StatusCreated, apigen.DualstackIdentity{
		Mac: it.Mac, Duid: it.Duid,
		Source: apigen.DualstackIdentitySource(it.Source),
		Note:   strPtr(it.Note), UpdatedAt: timePtr(it.UpdatedAt),
	})
}

// DeleteDualstackIdentity DELETE /dualstack/identities/{mac}
func (h *Handler) DeleteDualstackIdentity(c *gin.Context, mac string) {
	if err := h.store.DeleteIdentity(c.Request.Context(), normalizeIdentityKey(mac)); err != nil {
		problem.Write(c, http.StatusInternalServerError,
			"https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}

// ListDualstackConflicts GET /dualstack/conflicts
func (h *Handler) ListDualstackConflicts(c *gin.Context) {
	items, err := h.store.ListConflicts(c.Request.Context())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError,
			"https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	gen := make([]apigen.DualstackConflict, 0, len(items))
	for _, cf := range items {
		gen = append(gen, apigen.DualstackConflict{
			Duid: cf.Duid, V6Ip: strPtr(cf.V6Ip), Hostname: strPtr(cf.Hostname),
			V4Macs: &cf.V4Macs, Reason: apigen.DualstackConflictReason(cf.Reason),
			UpdatedAt: timePtr(cf.UpdatedAt),
		})
	}
	c.JSON(http.StatusOK, apigen.DualstackConflictList{Items: gen})
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func timePtr(t time.Time) *time.Time { return &t }
