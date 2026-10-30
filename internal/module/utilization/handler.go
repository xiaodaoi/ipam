package utilization

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	guuid "github.com/google/uuid"
	rtypes "github.com/oapi-codegen/runtime/types"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// Handler 利用率端点（M6-010B）。
type UtilHandler struct{ svc *Service }

func NewUtilHandler(svc *Service) *UtilHandler { return &UtilHandler{svc: svc} }

func toGen(p Point) apigen.SubnetUtilizationPoint {
	id, _ := guuid.Parse(p.SubnetID)
	out := apigen.SubnetUtilizationPoint{
		SubnetId: rtypes.UUID(id), Ts: p.TS, Used: p.Used, Capacity: p.Capacity, Pct: float32(p.Pct),
	}
	if p.SubnetName != "" {
		name := p.SubnetName
		out.SubnetName = &name
	}
	if p.CIDR != "" {
		cidr := p.CIDR
		out.Cidr = &cidr
	}
	return out
}

// ListSubnetUtilization GET /dhcp/utilization
func (h *UtilHandler) ListSubnetUtilization(c *gin.Context) {
	points, err := h.svc.Latest(c.Request.Context())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	items := make([]apigen.SubnetUtilizationPoint, 0, len(points))
	for _, p := range points {
		items = append(items, toGen(p))
	}
	c.JSON(http.StatusOK, apigen.SubnetUtilizationList{Items: items})
}

// GetSubnetUtilization GET /dhcp/utilization/{subnetId}
func (h *UtilHandler) GetSubnetUtilization(c *gin.Context, subnetId rtypes.UUID, params apigen.GetSubnetUtilizationParams) {
	hours := 24
	if params.Hours != nil {
		hours = *params.Hours
	}
	if hours < 1 || hours > 720 {
		hours = 24
	}
	points, err := h.svc.Series(c.Request.Context(), subnetId.String(), time.Now().UTC().Add(-time.Duration(hours)*time.Hour))
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	out := apigen.SubnetUtilizationSeries{SubnetId: subnetId, Points: make([]apigen.SubnetUtilizationPoint, 0, len(points))}
	for _, p := range points {
		if out.SubnetName == nil && p.SubnetName != "" {
			name := p.SubnetName
			out.SubnetName = &name
		}
		if out.Cidr == nil && p.CIDR != "" {
			cidr := p.CIDR
			out.Cidr = &cidr
		}
		out.Points = append(out.Points, toGen(p))
	}
	c.JSON(http.StatusOK, out)
}
