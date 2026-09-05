package dns

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	guuid "github.com/google/uuid"
	rtypes "github.com/oapi-codegen/runtime/types"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// ZoneHandler 实现 apigen.ServerInterface 中 zone/record 端点。
type ZoneHandler struct {
	svc    *ZoneService
	linked func(ctx context.Context, zoneName string) []LinkedRecord // 注入联动源
}

// NewZoneHandler linked 注入联动记录构建器（main 从 ipam 绑定装配）。
func NewZoneHandler(svc *ZoneService, linked func(context.Context, string) []LinkedRecord) *ZoneHandler {
	if linked == nil {
		linked = func(context.Context, string) []LinkedRecord { return nil }
	}
	return &ZoneHandler{svc: svc, linked: linked}
}

// LinkedRecord 联动记录（只读，§4.4）。
type LinkedRecord struct {
	Name    string
	RecType string
	Rdata   string
	MAC     string
}

func (h *ZoneHandler) ListDnsZones(c *gin.Context) {
	list, err := h.svc.repo.ListZones(c.Request.Context())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	items := make([]apigen.DnsZone, 0, len(list))
	for _, z := range list {
		items = append(items, apigen.DnsZone{
			Id: *uuidPtr(z.ID), Name: z.Name, Kind: apigen.DnsZoneKind(z.Kind), Enabled: z.Enabled,
		})
	}
	c.JSON(http.StatusOK, apigen.DnsZoneList{Items: items, Total: &[]int{len(items)}[0]})
}

func (h *ZoneHandler) CreateDnsZone(c *gin.Context) {
	var body apigen.DnsZoneCreate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	kind := "auth"
	if body.Kind != nil {
		kind = string(*body.Kind)
	}
	z, err := h.svc.CreateZone(c.Request.Context(), Zone{Name: body.Name, Kind: kind, Enabled: true})
	if err != nil {
		problem.Write(c, http.StatusConflict, "https://ipam.local/problems/zone-name-dup", "ZONE_NAME_DUP", "区域已存在")
		return
	}
	c.JSON(http.StatusCreated, apigen.DnsZone{Id: *uuidPtr(z.ID), Name: z.Name, Kind: apigen.DnsZoneKind(z.Kind), Enabled: z.Enabled})
}

func (h *ZoneHandler) DeleteDnsZone(c *gin.Context, zoneId rtypes.UUID) {
	if err := h.svc.DeleteZone(c.Request.Context(), zoneId.String()); err != nil {
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "ZONE_NOT_FOUND", "区域不存在")
		return
	}
	c.Status(http.StatusNoContent)
}

// UpdateDnsZone PATCH /dns/zones/{zoneId}
func (h *ZoneHandler) UpdateDnsZone(c *gin.Context, zoneId rtypes.UUID) {
	var body apigen.DnsZoneUpdate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	zones, err := h.svc.repo.ListZones(c.Request.Context())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "INTERNAL", err.Error())
		return
	}
	var cur *Zone
	for i := range zones {
		if zones[i].ID == zoneId.String() {
			cur = &zones[i]
		}
	}
	if cur == nil {
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "ZONE_NOT_FOUND", "区域不存在")
		return
	}
	if body.Name != nil {
		cur.Name = *body.Name
	}
	if body.Kind != nil {
		cur.Kind = string(*body.Kind)
	}
	if body.Enabled != nil {
		cur.Enabled = *body.Enabled
	}
	updated, err := h.svc.UpdateZone(c.Request.Context(), *cur)
	if err != nil {
		switch err {
		case ErrZoneNameDup:
			problem.Write(c, http.StatusConflict, "https://ipam.local/problems/zone-name-dup", "ZONE_NAME_DUP", "区域名已存在")
		default:
			problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "INTERNAL", err.Error())
		}
		return
	}
	c.JSON(http.StatusOK, apigen.DnsZone{Id: *uuidPtr(updated.ID), Name: updated.Name, Kind: apigen.DnsZoneKind(updated.Kind), Enabled: updated.Enabled})
}

func (h *ZoneHandler) ListDnsRecords(c *gin.Context, zoneId rtypes.UUID) {
	list, err := h.svc.repo.ListRecords(c.Request.Context(), zoneId.String())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	items := make([]apigen.DnsRecord, 0, len(list))
	for _, r := range list {
		items = append(items, toGenRecord(r))
	}
	c.JSON(http.StatusOK, apigen.DnsRecordList{Items: items, Total: &[]int{len(items)}[0]})
}

func (h *ZoneHandler) CreateDnsRecord(c *gin.Context, zoneId rtypes.UUID) {
	var body apigen.DnsRecordCreate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	r := Record{
		ZoneID:  zoneId.String(),
		Name:    body.Name,
		RecType: string(body.RecType),
		TTL:     deref(body.Ttl, 300),
		Rdata:   body.Rdata,
		Enabled: deref(body.Enabled, true),
	}
	saved, err := h.svc.CreateRecord(c.Request.Context(), zoneId.String(), r)
	if err != nil {
		switch err {
		case ErrRecordNameDup:
			problem.Write(c, http.StatusConflict, "https://ipam.local/problems/record-name-dup", "RECORD_NAME_DUP", "同名同类型记录已存在")
		case ErrBadRdata:
			problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_RDATA", err.Error())
		default:
			problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "INTERNAL", err.Error())
		}
		return
	}
	c.JSON(http.StatusCreated, toGenRecord(saved))
}

func (h *ZoneHandler) ExportDnsZone(c *gin.Context, zoneId rtypes.UUID) {
	zones, _ := h.svc.repo.ListZones(c.Request.Context())
	var zone *Zone
	for i := range zones {
		if zones[i].ID == zoneId.String() {
			zone = &zones[i]
		}
	}
	if zone == nil {
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "ZONE_NOT_FOUND", "区域不存在")
		return
	}
	records, _ := h.svc.repo.ListRecords(c.Request.Context(), zone.ID)
	c.Header("Content-Type", "text/plain")
	c.String(http.StatusOK, "%s", ExportZonefile(*zone, records))
}

// DeleteDnsRecord DELETE /dns/zones/{zoneId}/records/{recordId}
func (h *ZoneHandler) DeleteDnsRecord(c *gin.Context, zoneId rtypes.UUID, recordId rtypes.UUID) {
	if err := h.svc.DeleteRecord(c.Request.Context(), recordId.String()); err != nil {
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "RECORD_NOT_FOUND", "记录不存在")
		return
	}
	c.Status(http.StatusNoContent)
}

// UpdateDnsRecord PATCH /dns/zones/{zoneId}/records/{recordId}
func (h *ZoneHandler) UpdateDnsRecord(c *gin.Context, zoneId rtypes.UUID, recordId rtypes.UUID) {
	var body apigen.DnsRecordUpdate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	recs, err := h.svc.repo.ListRecords(c.Request.Context(), zoneId.String())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "INTERNAL", err.Error())
		return
	}
	var cur *Record
	for i := range recs {
		if recs[i].ID == recordId.String() {
			cur = &recs[i]
		}
	}
	if cur == nil {
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "RECORD_NOT_FOUND", "记录不存在")
		return
	}
	if body.Name != nil {
		cur.Name = *body.Name
	}
	if body.RecType != nil {
		cur.RecType = string(*body.RecType)
	}
	if body.Ttl != nil {
		cur.TTL = *body.Ttl
	}
	if body.Rdata != nil {
		cur.Rdata = *body.Rdata
	}
	if body.Enabled != nil {
		cur.Enabled = *body.Enabled
	}
	updated, err := h.svc.UpdateRecord(c.Request.Context(), *cur)
	if err != nil {
		switch err {
		case ErrRecordNameDup:
			problem.Write(c, http.StatusConflict, "https://ipam.local/problems/record-name-dup", "RECORD_NAME_DUP", "同名同类型记录已存在")
		case ErrBadRdata:
			problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_RDATA", err.Error())
		default:
			problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "INTERNAL", err.Error())
		}
		return
	}
	c.JSON(http.StatusOK, toGenRecord(updated))
}

func (h *ZoneHandler) ListLinkedRecords(c *gin.Context, zoneId rtypes.UUID) {
	zones, _ := h.svc.repo.ListZones(c.Request.Context())
	zoneName := ""
	for _, z := range zones {
		if z.ID == zoneId.String() {
			zoneName = z.Name
		}
	}
	if zoneName == "" {
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "ZONE_NOT_FOUND", "区域不存在")
		return
	}
	linked := h.linked(c.Request.Context(), zoneName)
	items := make([]apigen.LinkedRecord, 0, len(linked))
	for _, l := range linked {
		items = append(items, apigen.LinkedRecord{Name: l.Name, RecType: apigen.LinkedRecordRecType(l.RecType), Rdata: l.Rdata, Mac: strP(l.MAC)})
	}
	c.JSON(http.StatusOK, struct {
		Items []apigen.LinkedRecord `json:"items"`
	}{Items: items})
}

func toGenRecord(r Record) apigen.DnsRecord {
	return apigen.DnsRecord{
		Id:      *uuidPtr(r.ID),
		ZoneId:  *uuidPtr(r.ZoneID),
		Name:    r.Name,
		RecType: apigen.DnsRecordRecType(r.RecType),
		Ttl:     r.TTL,
		Rdata:   r.Rdata,
		Enabled: &r.Enabled,
	}
}

func uuidPtr(s string) *rtypes.UUID {
	u := guuid.MustParse(s)
	return (*rtypes.UUID)(&u)
}

var _ = context.Background
