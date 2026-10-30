package dhcp

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"regexp"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/engine/lease"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// LeaseAgent Kea 租约操作抽象（main 注入 *kea.CtrlAgent；测试用 fake）。
type LeaseAgent interface {
	Lease4List(ctx context.Context) ([]lease.Lease4, error)
	Lease4Get(ctx context.Context, ip string) (lease.Lease4, bool, error)
	Lease4Update(ctx context.Context, lease lease.Lease4) error
	Lease4Delete(ctx context.Context, ip string) error
	Lease6Delete(ctx context.Context, ip, leaseType string) error
}

// LeaseHandler 租约管理端点（M6-010）：v4 列表/延期/改绑/释放，v6 释放。
type LeaseHandler struct{ agent LeaseAgent }

func NewLeaseHandler(agent LeaseAgent) *LeaseHandler { return &LeaseHandler{agent: agent} }

var macRe = regexp.MustCompile(`^([0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}$`)

func toGenLease4(l lease.Lease4) apigen.DhcpLease4 {
	out := apigen.DhcpLease4{IpAddress: l.IPAddress}
	if l.HWAddress != "" {
		out.HwAddress = &l.HWAddress
	}
	if l.ClientID != "" {
		out.ClientId = &l.ClientID
	}
	if l.Hostname != "" {
		out.Hostname = &l.Hostname
	}
	if l.Cltt > 0 {
		v := int(l.Cltt)
		out.Cltt = &v
	}
	if l.ValidLifetime > 0 {
		v := int(l.ValidLifetime)
		out.ValidLifetime = &v
	}
	state := l.State
	out.State = &state
	if l.SubnetID > 0 {
		v := int(l.SubnetID)
		out.SubnetId = &v
	}
	if l.CircuitID != "" {
		v := l.CircuitID
		out.CircuitId = &v
	}
	if l.RemoteID != "" {
		v := l.RemoteID
		out.RemoteId = &v
	}
	return out
}

// ListDhcpLeases4 GET /dhcp/leases4
func (h *LeaseHandler) ListDhcpLeases4(c *gin.Context) {
	if h.agent == nil {
		c.JSON(http.StatusOK, apigen.DhcpLease4List{Items: []apigen.DhcpLease4{}})
		return
	}
	leases, err := h.agent.Lease4List(c.Request.Context())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "KEA_ERROR", err.Error())
		return
	}
	sort.Slice(leases, func(i, j int) bool {
		a, ea := netip.ParseAddr(leases[i].IPAddress)
		b, eb := netip.ParseAddr(leases[j].IPAddress)
		if ea != nil || eb != nil {
			return leases[i].IPAddress < leases[j].IPAddress
		}
		return a.Compare(b) < 0
	})
	items := make([]apigen.DhcpLease4, 0, len(leases))
	for _, l := range leases {
		items = append(items, toGenLease4(l))
	}
	c.JSON(http.StatusOK, apigen.DhcpLease4List{Items: items})
}

// UpdateDhcpLease4 PATCH /dhcp/leases4/{ipAddress}
func (h *LeaseHandler) UpdateDhcpLease4(c *gin.Context, ipAddress string) {
	if h.agent == nil {
		problem.Write(c, http.StatusServiceUnavailable, "https://ipam.local/problems/unavailable", "KEA_UNAVAILABLE", "Kea 未配置")
		return
	}
	var body apigen.DhcpLease4Update
	if err := c.ShouldBindJSON(&body); err != nil {
		badReq(c, "BAD_REQUEST", err.Error())
		return
	}
	if body.ValidLifetime == nil && body.HwAddress == nil && body.Hostname == nil {
		badReq(c, "BAD_REQUEST", "至少提供 validLifetime / hwAddress / hostname 之一")
		return
	}
	if body.ValidLifetime != nil && (*body.ValidLifetime < 60 || *body.ValidLifetime > 31536000) {
		badReq(c, "BAD_REQUEST", "validLifetime 需在 60-31536000 秒")
		return
	}
	if body.HwAddress != nil && !macRe.MatchString(strings.TrimSpace(*body.HwAddress)) {
		badReq(c, "BAD_REQUEST", "hwAddress 须为 aa:bb:cc:dd:ee:ff")
		return
	}
	if body.Hostname != nil && len(*body.Hostname) > 255 {
		badReq(c, "BAD_REQUEST", "hostname 过长")
		return
	}
	lease, ok, err := h.agent.Lease4Get(c.Request.Context(), ipAddress)
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "KEA_ERROR", err.Error())
		return
	}
	if !ok {
		notFound(c, "LEASE_NOT_FOUND")
		return
	}
	if body.ValidLifetime != nil {
		lease.ValidLifetime = uint32(*body.ValidLifetime)
	}
	if body.HwAddress != nil {
		lease.HWAddress = strings.ToLower(strings.TrimSpace(*body.HwAddress))
	}
	if body.Hostname != nil {
		lease.Hostname = strings.TrimSpace(*body.Hostname)
	}
	if err := h.agent.Lease4Update(c.Request.Context(), lease); err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "KEA_ERROR", err.Error())
		return
	}
	updated, ok, err := h.agent.Lease4Get(c.Request.Context(), ipAddress)
	if err != nil || !ok {
		c.JSON(http.StatusOK, toGenLease4(lease))
		return
	}
	c.JSON(http.StatusOK, toGenLease4(updated))
}

// DeleteDhcpLease4 DELETE /dhcp/leases4/{ipAddress}
func (h *LeaseHandler) DeleteDhcpLease4(c *gin.Context, ipAddress string) {
	if h.agent == nil {
		problem.Write(c, http.StatusServiceUnavailable, "https://ipam.local/problems/unavailable", "KEA_UNAVAILABLE", "Kea 未配置")
		return
	}
	if _, err := netip.ParseAddr(ipAddress); err != nil {
		badReq(c, "BAD_REQUEST", "IP 非法")
		return
	}
	if err := h.agent.Lease4Delete(c.Request.Context(), ipAddress); err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "KEA_ERROR", err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}

// DeleteDhcpLease6 DELETE /dhcp/leases6/{ipAddress}
func (h *LeaseHandler) DeleteDhcpLease6(c *gin.Context, ipAddress string, params apigen.DeleteDhcpLease6Params) {
	if h.agent == nil {
		problem.Write(c, http.StatusServiceUnavailable, "https://ipam.local/problems/unavailable", "KEA_UNAVAILABLE", "Kea 未配置")
		return
	}
	leaseType := string(params.LeaseType)
	if leaseType != "IA_NA" && leaseType != "IA_PD" {
		badReq(c, "BAD_REQUEST", "leaseType 仅支持 IA_NA/IA_PD")
		return
	}
	if err := h.agent.Lease6Delete(c.Request.Context(), ipAddress, leaseType); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "KEA_ERROR", err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}
