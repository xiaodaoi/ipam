package platform

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSecurityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(SecurityHeaders())
	r.GET("/", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Header().Get("X-Content-Type-Options") != "nosniff" ||
		w.Header().Get("X-Frame-Options") != "DENY" ||
		w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("安全头缺失: %v", w.Header())
	}
	if w.Header().Get("Strict-Transport-Security") != "" {
		t.Fatal("HTTP 请求不应下发 HSTS")
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.TLS = &tls.ConnectionState{}
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req)
	if w2.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("HTTPS 请求应下发 HSTS")
	}
}

func TestAPIRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(APIRateLimit(2))
	r.GET("/api/v1/ping", func(c *gin.Context) { c.String(200, "ok") })
	r.GET("/static", func(c *gin.Context) { c.String(200, "ok") })

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil))
		if w.Code != 200 {
			t.Fatalf("第 %d 次应放行: %d", i+1, w.Code)
		}
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil))
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("超限应 429 且有 Retry-After: %d %v", w.Code, w.Header())
	}
	// 静态资源不受 API 限流影响
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/static", nil))
	if w.Code != 200 {
		t.Fatalf("静态资源不应被 API 限流: %d", w.Code)
	}
}

func TestAPIRateLimitDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(APIRateLimit(0))
	r.GET("/api/v1/ping", func(c *gin.Context) { c.String(200, "ok") })
	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil))
		if w.Code != 200 {
			t.Fatalf("关闭限流后第 %d 次应 200: %d", i+1, w.Code)
		}
	}
}

func TestSecurityHeadersHardening(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(SecurityHeaders("default-src 'self'"))
	r.GET("/", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	checks := map[string]string{
		"Cross-Origin-Resource-Policy":      "same-origin",
		"X-Permitted-Cross-Domain-Policies": "none",
		"X-DNS-Prefetch-Control":            "off",
		"X-XSS-Protection":                  "0",
		"Content-Security-Policy":           "default-src 'self'",
	}
	for k, want := range checks {
		if got := w.Header().Get(k); got != want {
			t.Fatalf("头 %s=%q，want %q", k, got, want)
		}
	}
	// X-Forwarded-Proto 在未开启 IPAM_FORCE_HSTS 时不应触发 HSTS。
	if w.Header().Get("Strict-Transport-Security") != "" {
		t.Fatal("默认不应依据 XFF 下发 HSTS")
	}
}

func TestBodyLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(BodyLimit(16))
	r.POST("/api/v1/x", func(c *gin.Context) { c.String(200, "ok") })
	r.POST("/big", func(c *gin.Context) { c.String(200, "ok") })

	big := strings.Repeat("a", 64)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/x", strings.NewReader(big)))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("超限应 413，got %d", w.Code)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/x", strings.NewReader("tiny")))
	if w.Code != 200 {
		t.Fatalf("正常体应 200，got %d", w.Code)
	}

	// 非 /api/v1 不受限。
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/big", strings.NewReader(big)))
	if w.Code != 200 {
		t.Fatalf("非 API 路径不应受限，got %d", w.Code)
	}
}

func TestRejectTrace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RejectTrace())
	r.Handle(http.MethodTrace, "/x", func(c *gin.Context) { c.String(200, "ok") })
	r.GET("/x", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodTrace, "/x", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("TRACE 应 405，got %d", w.Code)
	}
}

func TestMetricsAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/metrics", MetricsAuth("s3cr3t"), func(c *gin.Context) { c.String(200, "m") })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("无令牌应 401，got %d", w.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer s3cr3t")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("正确令牌应 200，got %d", w.Code)
	}
}

func TestDefaultHTMLCSP(t *testing.T) {
	html := []byte(`<html><head><script>var x=1</script><script src="/a.js"></script></head></html>`)
	csp := DefaultHTMLCSP(html)
	if !strings.Contains(csp, "sha256-") {
		t.Fatalf("内联脚本应生成 sha256 哈希: %s", csp)
	}
	if strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
		t.Fatalf("脚本不应放行 unsafe-inline: %s", csp)
	}
	if !strings.Contains(csp, "frame-ancestors 'none'") || !strings.Contains(csp, "object-src 'none'") {
		t.Fatalf("CSP 缺少关键约束: %s", csp)
	}
	// 外链脚本不应被计入哈希（换行分隔的两个哈希只剩一个）。
	if strings.Count(csp, "sha256-") != 1 {
		t.Fatalf("仅内联脚本应计哈希: %s", csp)
	}
}
