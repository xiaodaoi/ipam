// Package platform 承载平台能力域：系统信息、健康探针、认证与审计。
// 对应主导航「系统管理」权限命名空间（§13.4）。
package platform

import (
	"net/http"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/hostmetrics"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// Handler 实现 apigen.ServerInterface 中 platform 域端点。
type Handler struct {
	version string
	started time.Time
	// dbProbe 为关键依赖（PostgreSQL）探针；nil 表示未接线，视为 ready。
	dbProbe func() error
	// hostMetrics 宿主资源采集器；nil=未接线，/system/metrics 返回空快照。
	hostMetrics *hostmetrics.Collector
}

// NewHandler 构造 platform 域 handler。version 由构建期 ldflags 注入。
func NewHandler(version string) *Handler {
	return &Handler{version: version, started: time.Now()}
}

// SetDBProbe 注入数据库就绪探针（M0-006 接线 PG 后生效）。
func (h *Handler) SetDBProbe(fn func() error) { h.dbProbe = fn }

// SetHostMetrics 注入宿主资源采集器（读只读挂载的宿主 /proc 等；仪表盘「主机资源」卡片数据源）。
func (h *Handler) SetHostMetrics(c *hostmetrics.Collector) { h.hostMetrics = c }

// GetSystemInfo GET /system/info —— 仪表盘服务健康卡片数据源与探活。
func (h *Handler) GetSystemInfo(c *gin.Context) {
	ready := h.dbProbe == nil || h.dbProbe() == nil
	c.JSON(http.StatusOK, apigen.SystemInfo{
		Name:      "ipam-control-plane",
		Version:   h.version,
		GoVersion: runtime.Version(),
		Now:       time.Now().UTC(),
		Ready:     ready,
	})
}

// GetSystemMetrics GET /system/metrics —— 宿主 CPU/内存/磁盘使用率快照（仪表盘「主机资源」卡片）。
func (h *Handler) GetSystemMetrics(c *gin.Context) {
	out := apigen.HostMetrics{Ts: time.Now().UTC(), Disks: []apigen.HostDisk{}}
	if h.hostMetrics == nil {
		c.JSON(http.StatusOK, out)
		return
	}
	m, err := h.hostMetrics.Collect()
	if err != nil {
		problem.Write(c, http.StatusServiceUnavailable,
			"https://ipam.local/problems/host-metrics-unavailable", "HOST_METRICS_UNAVAILABLE",
			"宿主资源采集不可用："+err.Error())
		return
	}
	out.Ts = m.TS
	l1, l5, l15 := float32(m.CPU.Load1), float32(m.CPU.Load5), float32(m.CPU.Load15)
	out.Cpu = apigen.HostCpu{
		UsagePct: float32(m.CPU.UsagePct),
		Cores:    m.CPU.Cores,
		Load1:    &l1, Load5: &l5, Load15: &l15,
	}
	up := float32(m.UptimeSec)
	out.UptimeSec = &up
	out.Memory = apigen.HostMemory{
		TotalBytes:     int64(m.Memory.TotalBytes),
		UsedBytes:      int64(m.Memory.UsedBytes),
		AvailableBytes: int64(m.Memory.AvailableBytes),
		UsedPct:        float32(m.Memory.UsedPct),
	}
	for _, d := range m.Disks {
		dev, fs := d.Device, d.FSType
		out.Disks = append(out.Disks, apigen.HostDisk{
			Mount:          d.Mount,
			Device:         &dev,
			FsType:         &fs,
			TotalBytes:     int64(d.TotalBytes),
			UsedBytes:      int64(d.UsedBytes),
			AvailableBytes: int64(d.AvailableBytes),
			UsedPct:        float32(d.UsedPct),
		})
	}
	c.JSON(http.StatusOK, out)
}

// WriteProblem 统一 RFC 9457 错误出口（委托共享包）。
func WriteProblem(c *gin.Context, status int, probType, code, detail string) {
	problem.Write(c, status, probType, code, detail)
}
