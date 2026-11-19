package dns

import (
	"errors"
	"net/http"

	rtypes "github.com/oapi-codegen/runtime/types"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// IntelHandler 暴露白名单与 RPZ 运行态端点（M6-038 P0）。
type IntelHandler struct {
	svc *IntelService
}

// NewIntelHandler 构造。
func NewIntelHandler(svc *IntelService) *IntelHandler { return &IntelHandler{svc: svc} }

// ListIntelWhitelist GET /dns/intel/whitelist
func (h *IntelHandler) ListIntelWhitelist(c *gin.Context) {
	list, err := h.svc.ListWhitelist(c.Request.Context())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	items := make([]apigen.IntelWhitelist, 0, len(list))
	for _, w := range list {
		items = append(items, toGenWhitelist(w))
	}
	c.JSON(http.StatusOK, apigen.IntelWhitelistList{Items: items, Total: &[]int{len(items)}[0]})
}

// CreateIntelWhitelist POST /dns/intel/whitelist（保存即生效）
func (h *IntelHandler) CreateIntelWhitelist(c *gin.Context) {
	var body apigen.IntelWhitelistCreate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	kind := "qname"
	if body.Kind != nil {
		kind = string(*body.Kind)
	}
	w := IntelWhitelist{
		Kind:      kind,
		Value:     body.Value,
		Reason:    body.Reason,
		Category:  derefStr(body.Category),
		Source:    "manual",
		ExpiresAt: body.ExpiresAt,
		Enabled:   true,
	}
	saved, _, err := h.svc.AddWhitelist(c.Request.Context(), w)
	switch {
	case errors.Is(err, ErrWhitelistInvalid):
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "WHITELIST_INVALID", "域名或豁免原因非法")
		return
	case errors.Is(err, ErrWhitelistDup):
		problem.Write(c, http.StatusConflict, "https://ipam.local/problems/conflict", "WHITELIST_DUP", "该域名已在白名单中")
		return
	case err != nil:
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.JSON(http.StatusCreated, toGenWhitelist(saved))
}

// DeleteIntelWhitelist DELETE /dns/intel/whitelist/{whitelistId}（恢复拦截）
func (h *IntelHandler) DeleteIntelWhitelist(c *gin.Context, whitelistId rtypes.UUID) {
	_, _, err := h.svc.DeleteWhitelist(c.Request.Context(), whitelistId.String())
	if errors.Is(err, ErrWhitelistNotFound) {
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "WHITELIST_NOT_FOUND", "白名单条目不存在")
		return
	}
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}

// ListIntelZones GET /dns/intel/zones
func (h *IntelHandler) ListIntelZones(c *gin.Context) {
	zones, err := h.svc.Zones(c.Request.Context())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	items := make([]apigen.IntelZone, 0, len(zones))
	for _, z := range zones {
		items = append(items, toGenIntelZone(z))
	}
	c.JSON(http.StatusOK, apigen.IntelZoneList{Items: items, Total: &[]int{len(items)}[0]})
}

// ReloadIntelZones POST /dns/intel/zones/reload（声明式重编译 + 单区热载）
func (h *IntelHandler) ReloadIntelZones(c *gin.Context) {
	res, err := h.svc.Compile(c.Request.Context())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "INTEL_COMPILE_FAILED", err.Error())
		return
	}
	items := make([]apigen.IntelZone, 0, len(res.Zones))
	for _, z := range res.Zones {
		items = append(items, toGenIntelZone(z))
	}
	reloaded := res.Reloaded
	c.JSON(http.StatusOK, apigen.IntelCompileResult{Zones: items, Entries: res.Entries, Reloaded: &reloaded})
}

func toGenWhitelist(w IntelWhitelist) apigen.IntelWhitelist {
	out := apigen.IntelWhitelist{
		Id:      *uuidPtr(w.ID),
		Kind:    apigen.IntelWhitelistKind(w.Kind),
		Value:   w.Value,
		Reason:  w.Reason,
		Enabled: w.Enabled,
	}
	if w.Category != "" {
		out.Category = &w.Category
	}
	if w.Source != "" {
		out.Source = &w.Source
	}
	out.ExpiresAt = w.ExpiresAt
	if !w.CreatedAt.IsZero() {
		out.CreatedAt = &w.CreatedAt
	}
	if w.CreatedBy != "" {
		out.CreatedBy = &w.CreatedBy
	}
	return out
}

func toGenIntelZone(z IntelZoneState) apigen.IntelZone {
	loaded := int(z.LoadedSerial)
	out := apigen.IntelZone{
		Zone:         z.Zone,
		Entries:      z.Entries,
		Serial:       int(z.Serial),
		LoadedSerial: &loaded,
		Status:       apigen.IntelZoneStatus(z.Status),
	}
	if z.SHA != "" {
		out.Sha256 = &z.SHA
	}
	if z.Path != "" {
		out.Path = &z.Path
	}
	if !z.UpdatedAt.IsZero() {
		out.UpdatedAt = &z.UpdatedAt
	}
	return out
}
