package dns

import (
	"context"
	"net/http"
	"time"

	guuid "github.com/google/uuid"
	rtypes "github.com/oapi-codegen/runtime/types"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// DnsHandler 实现 apigen.ServerInterface 中 dns 域端点（§13.4 DNS 服务）。
type DnsHandler struct {
	svc *Service
}

func NewDnsHandler(svc *Service) *DnsHandler { return &DnsHandler{svc: svc} }

// ListUpstreams GET /upstreams
func (h *DnsHandler) ListUpstreams(c *gin.Context) {
	list, health, err := h.svc.List(c.Request.Context())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	items := make([]apigen.Upstream, 0, len(list))
	for i, u := range list {
		items = append(items, toGenUpstream(u, health[i]))
	}
	c.JSON(http.StatusOK, apigen.UpstreamList{Items: items, Total: &[]int{len(items)}[0]})
}

// CreateUpstream POST /upstreams
func (h *DnsHandler) CreateUpstream(c *gin.Context) {
	var body apigen.UpstreamCreate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	protocol := "udp"
	if body.Protocol != nil {
		protocol = string(*body.Protocol)
	}
	u := Upstream{
		Name:     body.Name,
		Addrs:    body.Addrs,
		Protocol: protocol,
		Weight:   deref(body.Weight, 1),
		Enabled:  deref(body.Enabled, true),
	}
	saved, err := h.svc.Create(c.Request.Context(), u)
	if err != nil {
		problem.Write(c, http.StatusServiceUnavailable, "https://ipam.local/problems/unbound-down", "UNBOUND_DOWN", "上游已保存但 unbound 下发失败")
		return
	}
	c.JSON(http.StatusCreated, toGenUpstream(saved, Health{}))
}

// UpdateUpstream PATCH /upstreams/{upstreamId}
func (h *DnsHandler) UpdateUpstream(c *gin.Context, upstreamId rtypes.UUID) {
	var body apigen.UpstreamUpdate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	cur, ok, err := h.svc.repo.Get(c.Request.Context(), rtypes.UUID(upstreamId).String())
	if err != nil || !ok {
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "UPSTREAM_NOT_FOUND", "上游不存在")
		return
	}
	if body.Name != nil {
		cur.Name = *body.Name
	}
	if body.Addrs != nil {
		cur.Addrs = *body.Addrs
	}
	if body.Protocol != nil {
		cur.Protocol = string(*body.Protocol)
	}
	if body.Weight != nil {
		cur.Weight = *body.Weight
	}
	if body.Enabled != nil {
		cur.Enabled = *body.Enabled
	}
	if err := h.svc.repo.Update(c.Request.Context(), cur); err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	_ = h.svc.resync(c.Request.Context())
	c.JSON(http.StatusOK, toGenUpstream(cur, h.svc.prober.Status(cur.ID)))
}

// DeleteUpstream DELETE /upstreams/{upstreamId}
func (h *DnsHandler) DeleteUpstream(c *gin.Context, upstreamId rtypes.UUID) {
	if err := h.svc.repo.Delete(c.Request.Context(), rtypes.UUID(upstreamId).String()); err != nil {
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "UPSTREAM_NOT_FOUND", "上游不存在")
		return
	}
	_ = h.svc.resync(c.Request.Context())
	c.Status(http.StatusNoContent)
}

func toGenUpstream(u Upstream, h Health) apigen.Upstream {
	protocol := apigen.UpstreamProtocol(u.Protocol)
	up := h.Up
	rtt := h.RTTMs
	fails := h.ConsecutiveFails
	last := h.LastCheck
	upstream := apigen.Upstream{
		Id:       rtypes.UUID(guuid.MustParse(u.ID)),
		Name:     u.Name,
		Addrs:    u.Addrs,
		Protocol: protocol,
		Weight:   u.Weight,
		Enabled:  u.Enabled,
		Health: struct {
			ConsecutiveFails *int       `json:"consecutiveFails,omitempty"`
			LastCheck        *time.Time `json:"lastCheck,omitempty"`
			RttMs            *int       `json:"rttMs,omitempty"`
			Up               *bool      `json:"up,omitempty"`
		}{ConsecutiveFails: &fails, LastCheck: &last, RttMs: &rtt, Up: &up},
	}
	return upstream
}

func deref[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

var _ = context.Background
