// Package platform 承载平台能力域：系统信息、健康探针、认证与审计。
// 对应主导航「系统管理」权限命名空间（§13.4）。
package platform

import (
	"net/http"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
)

// Handler 实现 apigen.ServerInterface 中 platform 域端点。
type Handler struct {
	version string
	started time.Time
	// dbProbe 为关键依赖（PostgreSQL）探针；nil 表示未接线，视为 ready。
	dbProbe func() error
}

// NewHandler 构造 platform 域 handler。version 由构建期 ldflags 注入。
func NewHandler(version string) *Handler {
	return &Handler{version: version, started: time.Now()}
}

// SetDBProbe 注入数据库就绪探针（M0-006 接线 PG 后生效）。
func (h *Handler) SetDBProbe(fn func() error) { h.dbProbe = fn }

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

// WriteProblem 统一 RFC 9457 错误出口（§12.2 约定 4：code 供 AI 自纠重试）。
func WriteProblem(c *gin.Context, status int, probType, code, detail string) {
	p := apigen.Problem{
		Type:   probType,
		Title:  http.StatusText(status),
		Status: status,
	}
	if code != "" {
		p.Code = &code
	}
	if detail != "" {
		p.Detail = &detail
	}
	if c.Request != nil && c.Request.URL != nil {
		p.Instance = &c.Request.URL.Path
	}
	c.Header("Content-Type", "application/problem+json")
	c.JSON(status, p)
}
