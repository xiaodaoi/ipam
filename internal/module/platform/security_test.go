package platform

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
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
