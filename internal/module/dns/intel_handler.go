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

// ── 情报源（M6-039）──

// ListIntelSources GET /dns/intel/sources
func (h *IntelHandler) ListIntelSources(c *gin.Context) {
	list, err := h.svc.Sources(c.Request.Context())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	items := make([]apigen.IntelSource, 0, len(list))
	for _, s := range list {
		items = append(items, toGenSource(s))
	}
	total := len(items)
	c.JSON(http.StatusOK, apigen.IntelSourceList{Items: items, Total: &total})
}

// CreateIntelSource POST /dns/intel/sources（保存为关闭状态）
func (h *IntelHandler) CreateIntelSource(c *gin.Context) {
	var body apigen.IntelSourceCreate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	layer := "hot"
	if body.Layer != nil {
		layer = string(*body.Layer)
	}
	src := IntelSource{
		Name:           body.Name,
		URL:            body.Url,
		Layer:          layer,
		License:        derefStr(body.License),
		UpdateInterval: 21600,
	}
	if body.UpdateInterval != nil {
		src.UpdateInterval = *body.UpdateInterval
	}
	saved, err := h.svc.CreateSource(c.Request.Context(), src)
	switch {
	case errors.Is(err, ErrSourceNoURL):
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "INTEL_SOURCE_INVALID", "名称必填，URL 需以 http(s):// 开头")
		return
	case errors.Is(err, ErrSourceDupKey):
		problem.Write(c, http.StatusConflict, "https://ipam.local/problems/conflict", "INTEL_SOURCE_DUP", "同名情报源已存在")
		return
	case err != nil:
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.JSON(http.StatusCreated, toGenSource(saved))
}

// UpdateIntelSource PATCH /dns/intel/sources/{sourceId}
func (h *IntelHandler) UpdateIntelSource(c *gin.Context, sourceId rtypes.UUID) {
	var body apigen.IntelSourceUpdate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	up := SourceUpdate{
		Name:           body.Name,
		URL:            body.Url,
		AuthHeader:     body.AuthHeader,
		Secret:         body.Secret,
		UpdateInterval: body.UpdateInterval,
		Enabled:        body.Enabled,
		AckLicense:     body.AckLicense != nil && *body.AckLicense,
		ClearSecret:    body.ClearSecret != nil && *body.ClearSecret,
	}
	if body.Layer != nil {
		l := string(*body.Layer)
		up.Layer = &l
	}
	saved, err := h.svc.UpdateSource(c.Request.Context(), sourceId.String(), up)
	switch {
	case errors.Is(err, ErrSourceNotFound):
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "INTEL_SOURCE_NOT_FOUND", "情报源不存在")
		return
	case errors.Is(err, ErrSourceLicenseAck):
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "INTEL_SOURCE_LICENSE_UNCONFIRMED", "启用前必须确认许可条款（含商用限制）")
		return
	case errors.Is(err, ErrSourceSecretNeeded):
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "INTEL_SOURCE_SECRET_REQUIRED", "该源需要鉴权密钥，请先填写密钥")
		return
	case errors.Is(err, ErrSourceNoURL):
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "INTEL_SOURCE_NO_URL", "请先填写有效的拉取 URL")
		return
	case errors.Is(err, ErrSecretDirUnwritable):
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "INTEL_SECRET_DIR_UNWRITABLE", err.Error())
		return
	case errors.Is(err, ErrSourceEmpty):
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "INTEL_SOURCE_EMPTY", "已启用，但首次同步未解析到条目（详见 lastError）")
		return
	case err != nil:
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.JSON(http.StatusOK, toGenSource(saved))
}

// DeleteIntelSource DELETE /dns/intel/sources/{sourceId}（仅自定义源）
func (h *IntelHandler) DeleteIntelSource(c *gin.Context, sourceId rtypes.UUID) {
	err := h.svc.DeleteSource(c.Request.Context(), sourceId.String())
	switch {
	case errors.Is(err, ErrSourceNotFound):
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "INTEL_SOURCE_NOT_FOUND", "情报源不存在")
		return
	case errors.Is(err, ErrSourceBuiltin):
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "INTEL_SOURCE_BUILTIN", "预设源不可删除，可停用")
		return
	case err != nil:
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}

// SyncIntelSource POST /dns/intel/sources/{sourceId}/sync（失败保留上一版）
func (h *IntelHandler) SyncIntelSource(c *gin.Context, sourceId rtypes.UUID) {
	src, err := h.svc.SyncSource(c.Request.Context(), sourceId.String())
	switch {
	case errors.Is(err, ErrSourceNotFound):
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "INTEL_SOURCE_NOT_FOUND", "情报源不存在")
		return
	case err != nil:
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "INTEL_SOURCE_SYNC_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, toGenSource(src))
}

func toGenSource(s IntelSource) apigen.IntelSource {
	out := apigen.IntelSource{
		Id:             *uuidPtr(s.ID),
		Key:            s.Key,
		Name:           s.Name,
		Kind:           apigen.IntelSourceKind(s.Kind),
		Layer:          apigen.IntelSourceLayer(s.Layer),
		Enabled:        s.Enabled,
		UpdateInterval: s.UpdateInterval,
	}
	out.Url = strPtr(s.URL)
	out.Format = strPtr(s.Format)
	out.AuthHeader = strPtr(s.AuthHeader)
	out.HasSecret = &s.HasSecret
	out.License = strPtr(s.License)
	out.LicenseUrl = strPtr(s.LicenseURL)
	out.CommercialOk = &s.CommercialOK
	out.RequiresAck = &s.RequiresAck
	out.AcknowledgedAt = s.AckedAt
	out.Entries = &s.Entries
	out.LastSyncAt = s.LastSyncAt
	status := apigen.IntelSourceLastStatus(s.LastStatus)
	out.LastStatus = &status
	out.LastError = strPtr(s.LastError)
	out.Builtin = &s.Builtin
	if !s.CreatedAt.IsZero() {
		out.CreatedAt = &s.CreatedAt
	}
	if !s.UpdatedAt.IsZero() {
		out.UpdatedAt = &s.UpdatedAt
	}
	return out
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
