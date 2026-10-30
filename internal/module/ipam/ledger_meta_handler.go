package ipam

import (
	"net/http"
	"net/netip"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// LedgerMetaHandler 逐 IP 台账元数据端点（M6-010B3）。
type LedgerMetaHandler struct{ repo LedgerMetaRepo }

func NewLedgerMetaHandler(repo LedgerMetaRepo) *LedgerMetaHandler {
	return &LedgerMetaHandler{repo: repo}
}

// UpsertLedgerMeta PUT /ledger/meta
func (h *LedgerMetaHandler) UpsertLedgerMeta(c *gin.Context) {
	var body apigen.LedgerMetaUpsert
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	addr, err := netip.ParseAddr(body.Address)
	if err != nil || !addr.Is4() {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", "address 须为 IPv4")
		return
	}
	m := LedgerMeta{
		Address: addr.String(),
		Site:    derefStr(body.Site),
		Purpose: derefStr(body.Purpose),
		User:    derefStr(body.User),
		Phone:   derefStr(body.Phone),
		Remark:  derefStr(body.Remark),
	}
	if body.SubnetId != nil {
		m.SubnetID = body.SubnetId.String()
	}
	if body.Vlan != nil {
		if *body.Vlan < 0 || *body.Vlan > 4094 {
			problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", "vlan 需在 0-4094")
			return
		}
		m.VLAN = *body.Vlan
	}
	if body.CustomAttrs != nil {
		m.CustomAttrs = *body.CustomAttrs
	}
	if err := h.repo.Upsert(c.Request.Context(), m); err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	body.Address = m.Address
	c.JSON(http.StatusOK, body)
}
