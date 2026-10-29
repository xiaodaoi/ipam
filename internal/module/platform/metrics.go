package platform

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/xiaodaoi/ipam/internal/pkg/hostmetrics"
)

// MetricsSources 指标数据源（M6-005）：组件探针可空，空则跳过对应指标。
type MetricsSources struct {
	Version     string
	Started     time.Time
	Postgres    func(context.Context) error
	ClickHouse  func(context.Context) error
	Kea         func(context.Context) error
	Unbound     func(context.Context) error
	Host        *hostmetrics.Collector
	TLSCertFile string
}

// MetricsHandler 暴露 Prometheus 文本格式指标，并统计 HTTP 请求。
type MetricsHandler struct {
	src MetricsSources

	mu      sync.Mutex
	reqs    map[string]uint64
	durSum  float64
	durCnt  uint64
	started time.Time
}

func NewMetricsHandler(src MetricsSources) *MetricsHandler {
	if src.Started.IsZero() {
		src.Started = time.Now()
	}
	return &MetricsHandler{src: src, started: src.Started, reqs: map[string]uint64{}}
}

// Middleware 统计 /api/v1 请求数/耗时；/metrics 自身不计入，避免噪声。
func (h *MetricsHandler) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/metrics" {
			c.Next()
			return
		}
		start := time.Now()
		c.Next()
		h.mu.Lock()
		h.reqs[fmt.Sprintf("method=%q,status=%q", c.Request.Method, fmt.Sprintf("%d", c.Writer.Status()))]++
		h.durSum += time.Since(start).Seconds()
		h.durCnt++
		h.mu.Unlock()
	}
}

// Serve GET /metrics。
func (h *MetricsHandler) Serve(c *gin.Context) {
	var b strings.Builder
	fmt.Fprintf(&b, "# HELP ipam_build_info 构建信息\n# TYPE ipam_build_info gauge\n")
	fmt.Fprintf(&b, "ipam_build_info{version=%q} 1\n", h.src.Version)
	fmt.Fprintf(&b, "# HELP ipam_uptime_seconds 控制面运行时长（秒）\n# TYPE ipam_uptime_seconds gauge\n")
	fmt.Fprintf(&b, "ipam_uptime_seconds %.0f\n", time.Since(h.started).Seconds())

	h.mu.Lock()
	reqKeys := make([]string, 0, len(h.reqs))
	for k := range h.reqs {
		reqKeys = append(reqKeys, k)
	}
	sort.Strings(reqKeys)
	fmt.Fprintf(&b, "# HELP ipam_http_requests_total HTTP 请求总数\n# TYPE ipam_http_requests_total counter\n")
	for _, k := range reqKeys {
		fmt.Fprintf(&b, "ipam_http_requests_total{%s} %d\n", k, h.reqs[k])
	}
	fmt.Fprintf(&b, "# HELP ipam_http_request_duration_seconds HTTP 请求耗时\n# TYPE ipam_http_request_duration_seconds summary\n")
	fmt.Fprintf(&b, "ipam_http_request_duration_seconds_sum %.6f\n", h.durSum)
	fmt.Fprintf(&b, "ipam_http_request_duration_seconds_count %d\n", h.durCnt)
	h.mu.Unlock()

	fmt.Fprintf(&b, "# HELP ipam_component_up 组件连通性（1=正常 0=异常）\n# TYPE ipam_component_up gauge\n")
	probe := func(component string, fn func(context.Context) error) {
		if fn == nil {
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 1500*time.Millisecond)
		defer cancel()
		v := 1
		if err := fn(ctx); err != nil {
			v = 0
		}
		fmt.Fprintf(&b, "ipam_component_up{component=%q} %d\n", component, v)
	}
	probe("postgres", h.src.Postgres)
	probe("clickhouse", h.src.ClickHouse)
	probe("kea", h.src.Kea)
	probe("unbound", h.src.Unbound)

	if h.src.TLSCertFile != "" {
		if raw, err := os.ReadFile(h.src.TLSCertFile); err == nil {
			if block, _ := pem.Decode(raw); block != nil {
				if cert, err := x509.ParseCertificate(block.Bytes); err == nil {
					fmt.Fprintf(&b, "# HELP ipam_tls_cert_expiry_timestamp_seconds TLS 证书到期时间（Unix 秒）\n# TYPE ipam_tls_cert_expiry_timestamp_seconds gauge\n")
					fmt.Fprintf(&b, "ipam_tls_cert_expiry_timestamp_seconds %d\n", cert.NotAfter.Unix())
				}
			}
		}
	}

	if h.src.Host != nil {
		if m, err := h.src.Host.Collect(); err == nil {
			fmt.Fprintf(&b, "# HELP ipam_host_cpu_usage_percent 宿主 CPU 使用率\n# TYPE ipam_host_cpu_usage_percent gauge\n")
			fmt.Fprintf(&b, "ipam_host_cpu_usage_percent %.2f\n", m.CPU.UsagePct)
			fmt.Fprintf(&b, "# HELP ipam_host_cpu_cores 宿主 CPU 核数\n# TYPE ipam_host_cpu_cores gauge\n")
			fmt.Fprintf(&b, "ipam_host_cpu_cores %d\n", m.CPU.Cores)
			fmt.Fprintf(&b, "# HELP ipam_host_memory_used_bytes 宿主已用内存\n# TYPE ipam_host_memory_used_bytes gauge\n")
			fmt.Fprintf(&b, "ipam_host_memory_used_bytes %d\n", m.Memory.UsedBytes)
			fmt.Fprintf(&b, "# HELP ipam_host_memory_total_bytes 宿主总内存\n# TYPE ipam_host_memory_total_bytes gauge\n")
			fmt.Fprintf(&b, "ipam_host_memory_total_bytes %d\n", m.Memory.TotalBytes)
			fmt.Fprintf(&b, "# HELP ipam_host_disk_used_percent 宿主文件系统使用率\n# TYPE ipam_host_disk_used_percent gauge\n")
			for _, d := range m.Disks {
				fmt.Fprintf(&b, "ipam_host_disk_used_percent{mount=%q,device=%q,fstype=%q} %.2f\n", d.Mount, d.Device, d.FSType, d.UsedPct)
			}
			fmt.Fprintf(&b, "# HELP ipam_host_uptime_seconds 宿主运行时长\n# TYPE ipam_host_uptime_seconds gauge\n")
			fmt.Fprintf(&b, "ipam_host_uptime_seconds %.0f\n", m.UptimeSec)
		}
	}

	c.Header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	c.String(http.StatusOK, b.String())
}
