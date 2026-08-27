package dashboard

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	uuid "github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// Handler 实现 apigen.ServerInterface 中 GET /dashboard。
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// GetDashboardOverview 仪表盘单请求聚合快照。
func (h *Handler) GetDashboardOverview(c *gin.Context, params apigen.GetDashboardOverviewParams) {
	n := 5
	if params.PoolTopN != nil {
		n = *params.PoolTopN
	}
	if n < 1 || n > 20 {
		n = 5
	}
	ov, err := h.svc.Overview(c.Request.Context(), n)
	if err != nil {
		problem.Write(c, http.StatusInternalServerError,
			"https://ipam.local/problems/internal", "DASHBOARD_ERROR", err.Error())
		return
	}
	c.JSON(http.StatusOK, toGenOverview(*ov))
}

type genTrendPoint = struct {
	Count int       `json:"count"`
	Ts    time.Time `json:"ts"`
}

func toGenOverview(ov Overview) apigen.DashboardOverview {
	trend := make([]genTrendPoint, 0, len(ov.OnlineTrend))
	for _, p := range ov.OnlineTrend {
		trend = append(trend, genTrendPoint{Count: p.Count, Ts: p.TS})
	}

	gen := apigen.DashboardOverview{
		Ts:                      ov.TS,
		TodayStart:              &ov.TodayStart,
		OnlineNow:               int(ov.OnlineNow),
		OnlineTrend:             trend,
		NewTerminals:            int64Ptr(ov.NewTerminals),
		OfflineTerminals:        int64Ptr(ov.OfflineTerminals),
		CoherenceSuccessRatePct: int64Ptr(ov.CoherencePct),
		PoolUtilTop:             genPoolTop(ov.PoolTop),
	}
	gen.Services.Postgres = apigen.HealthLight(ov.Lights["postgres"])
	gen.Services.Clickhouse = apigen.HealthLight(ov.Lights["clickhouse"])
	gen.Services.Kea = apigen.HealthLight(ov.Lights["kea"])
	gen.Services.Unbound = apigen.HealthLight(ov.Lights["unbound"])

	dns := struct {
		BlockedToday *int     `json:"blockedToday,omitempty"`
		HitRatePct   *int     `json:"hitRatePct,omitempty"`
		Qps5m        *float32 `json:"qps5m,omitempty"`
	}{HitRatePct: int64Ptr(ov.HitRatePct)}
	if ov.Qps5m != nil {
		q := float32(*ov.Qps5m)
		dns.Qps5m = &q
	}
	if ov.BlockedToday != nil {
		b := int(*ov.BlockedToday)
		dns.BlockedToday = &b
	}
	gen.Dns = &dns
	return gen
}

func genPoolTop(rows []PoolUtil) *[]apigen.PoolUtilization {
	items := make([]apigen.PoolUtilization, 0, len(rows))
	for _, r := range rows {
		id, _ := uuid.Parse(r.SubnetID)
		subnetID := openapi_types.UUID(id)
		items = append(items, apigen.PoolUtilization{
			SubnetId:  subnetID,
			Name:      r.Name,
			Cidr:      r.CIDR,
			PoolIndex: strconv.Itoa(r.PoolIdx),
			Used:      int(r.Used),
			Capacity:  int(clampInt(r.Capacity)),
			Pct:       float32(r.Pct),
		})
	}
	return &items
}

func clampInt(v int64) int64 {
	const maxInt = int64(^uint(0) >> 1)
	if v > maxInt {
		return maxInt
	}
	return v
}

func int64Ptr(p *int64) *int {
	if p == nil {
		return nil
	}
	v := int(*p)
	return &v
}
