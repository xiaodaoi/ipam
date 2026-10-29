package platform

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestMetricsHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewMetricsHandler(MetricsSources{
		Version:    "9.9.9-test",
		Started:    time.Now().Add(-2 * time.Second),
		Postgres:   func(context.Context) error { return nil },
		ClickHouse: func(context.Context) error { return errors.New("down") },
	})
	r := gin.New()
	r.Use(h.Middleware())
	r.GET("/api/v1/ping", func(c *gin.Context) { c.String(200, "ok") })
	r.GET("/metrics", h.Serve)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil))
	if w.Code != 200 {
		t.Fatalf("ping %d", w.Code)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("metrics %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	body := w.Body.String()
	for _, want := range []string{
		`ipam_build_info{version="9.9.9-test"} 1`,
		`ipam_http_requests_total{method="GET",status="200"} 1`,
		`ipam_component_up{component="postgres"} 1`,
		`ipam_component_up{component="clickhouse"} 0`,
		"ipam_uptime_seconds",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in metrics:\n%s", want, body)
		}
	}
	// /metrics 自身不应被计入请求数
	if strings.Contains(body, `status="200"} 2`) {
		t.Fatalf("metrics endpoint should not be counted: %s", body)
	}
}
