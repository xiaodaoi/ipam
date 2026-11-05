package systemmetric

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// MetricHandler 指标历史端点（M6-032）。
type MetricHandler struct{ svc *Service }

func NewHandler(svc *Service) *MetricHandler { return &MetricHandler{svc: svc} }

func f32(v float64) *float32 { x := float32(v); return &x }
func iptr(v int) *int        { x := v; return &x }
func i64(v int64) *int64     { x := v; return &x }
func tsPtr(v time.Time) *time.Time {
	if v.IsZero() {
		return nil
	}
	x := v.UTC()
	return &x
}
func strPtr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func toPoint(p Point) apigen.SystemMetricPoint {
	return apigen.SystemMetricPoint{
		Ts:              p.TS,
		CpuPct:          f32(p.CPUPct),
		Cores:           iptr(p.Cores),
		Load1:           f32(p.Load1),
		Load5:           f32(p.Load5),
		Load15:          f32(p.Load15),
		MemUsedBytes:    i64(p.MemUsedBytes),
		MemTotalBytes:   i64(p.MemTotalBytes),
		MemPct:          f32(p.MemPct),
		UptimeSec:       f32(p.UptimeSec),
		DiskRootPct:     f32(p.DiskRootPct),
		DiskMaxPct:      f32(p.DiskMaxPct),
		DiskWorstMount:  strPtr(p.DiskWorstMount),
		Goroutines:      iptr(p.Goroutines),
		HeapBytes:       i64(p.HeapBytes),
		DbPoolInUse:     iptr(p.DBPoolInUse),
		DbPoolMax:       iptr(p.DBPoolMax),
		LogLatestAgeSec: i64(p.LogLatestAgeSec),
	}
}

// GetSystemMetricsHistory GET /system/metrics/history
func (h *MetricHandler) GetSystemMetricsHistory(c *gin.Context, params apigen.GetSystemMetricsHistoryParams) {
	now := time.Now().UTC()
	var from, to time.Time
	if params.From != nil {
		from = params.From.UTC()
	}
	if params.To != nil {
		to = params.To.UTC()
	}
	var step time.Duration
	if params.Step != nil {
		step = time.Duration(*params.Step) * time.Second
	}
	from, to, step, ok := NormalizeRange(from, to, step, now)
	if !ok {
		problem.Write(c, http.StatusBadRequest,
			"https://ipam.local/problems/metric-range-invalid", "METRIC_RANGE_INVALID",
			"查询窗口需满足 from<to 且不超过 90 天")
		return
	}
	points, err := h.svc.History(c.Request.Context(), from, to, step)
	if err != nil {
		problem.Write(c, http.StatusInternalServerError,
			"https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	items := make([]apigen.SystemMetricPoint, 0, len(points))
	for _, p := range points {
		items = append(items, toPoint(p))
	}
	// 显式回带实际窗口，便于前端提示「step 已被放大」。
	c.Header("X-Metric-Window-From", from.Format(time.RFC3339))
	c.Header("X-Metric-Window-Step", step.String())
	c.JSON(http.StatusOK, apigen.SystemMetricHistory{Items: items})
}

// GetSystemMetricsSummary GET /system/metrics/summary
func (h *MetricHandler) GetSystemMetricsSummary(c *gin.Context, params apigen.GetSystemMetricsSummaryParams) {
	hours := 24
	if params.Hours != nil && *params.Hours > 0 && *params.Hours <= 2160 {
		hours = *params.Hours
	}
	now := time.Now().UTC()
	from := now.Add(-time.Duration(hours) * time.Hour)
	sum, err := h.svc.Summary(c.Request.Context(), from)
	if err != nil {
		problem.Write(c, http.StatusInternalServerError,
			"https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	out := apigen.SystemMetricSummary{
		Node:               sum.Node,
		From:               from,
		To:                 sum.To,
		Samples:            sum.Samples,
		Since:              tsPtr(sum.Since),
		CpuMax:             f32(sum.CPUMax),
		CpuMaxAt:           tsPtr(sum.CPUMaxAt),
		MemMax:             f32(sum.MemMax),
		MemMaxAt:           tsPtr(sum.MemMaxAt),
		DiskMax:            f32(sum.DiskMax),
		DiskMaxAt:          tsPtr(sum.DiskMaxAt),
		DiskMaxMount:       strPtr(sum.DiskMount),
		Load1Max:           f32(sum.Load1Max),
		Load1MaxAt:         tsPtr(sum.Load1MaxAt),
		CpuAvg:             f32(sum.CPUAvg),
		MemAvg:             f32(sum.MemAvg),
		DiskAvg:            f32(sum.DiskAvg),
		GoroutinesMax:      iptr(sum.GoroutinesMax),
		HeapMaxBytes:       i64(sum.HeapMaxBytes),
		DbPoolInUseMax:     iptr(sum.DBPoolInUseMax),
		LogLatestAgeMaxSec: i64(sum.LogLatestAgeMaxSec),
	}
	c.JSON(http.StatusOK, out)
}
