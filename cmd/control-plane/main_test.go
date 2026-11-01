package main

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
)

func TestRoutesCoveredBySpec(t *testing.T) {
	sw, err := apigen.GetSpec()
	if err != nil {
		t.Fatalf("load embedded spec: %v", err)
	}
	r := newEngine("test", context.Background())
	for _, ri := range r.Routes() {
		if !strings.HasPrefix(ri.Path, "/api/v1/") {
			continue
		}
		p := strings.TrimPrefix(ri.Path, "/api/v1")
		item := sw.Paths.Find(normalizeToOpenAPIPath(p))
		if item == nil {
			t.Errorf("route %s %s 未在 spec 中文档化（缺 paths.%s）", ri.Method, p, p)
			continue
		}
		if op := item.GetOperation(ri.Method); op == nil {
			t.Errorf("route %s %s 存在 path 但缺少对应 operation", ri.Method, p)
		}
	}
}

func normalizeToOpenAPIPath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		if strings.HasPrefix(s, ":") || strings.HasPrefix(s, "*") {
			segs[i] = "{" + s[1:] + "}"
		}
	}
	return strings.Join(segs, "/")
}

func TestSPAIndexServed(t *testing.T) {
	r := newEngine("test", context.Background())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("content-type = %q, want text/html", w.Header().Get("Content-Type"))
	}
}

func TestSPAFallbackDeepRoute(t *testing.T) {
	r := newEngine("test", context.Background())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/dashboard/analytics", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("fallback failed: code=%d ct=%q", w.Code, w.Header().Get("Content-Type"))
	}
}

func TestUnknownAPIRouteReturnsProblem(t *testing.T) {
	r := newEngine("test", context.Background())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/definitely-not-exist", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content-type = %q, want problem+json", ct)
	}
	if !strings.Contains(w.Body.String(), "API_ROUTE_NOT_FOUND") {
		t.Fatalf("missing machine-readable code: %s", w.Body.String())
	}
}

// M6-017：引擎级安全加固接线（CSP、TRACE、请求体上限）。
func TestEngineSecurityHardening(t *testing.T) {
	t.Setenv("IPAM_MAX_BODY_BYTES", "1024")
	r := newEngine("test", context.Background())

	// index.html 响应带默认 CSP（含内联脚本 sha256，无 script unsafe-inline）。
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Fatalf("CSP 未生效: %q", csp)
	}
	if strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
		t.Fatalf("脚本不应放行 unsafe-inline: %q", csp)
	}

	// TRACE → 405
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodTrace, "/", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("TRACE 应 405，got %d", w.Code)
	}

	// /api/v1 超大请求体 → 413
	big := strings.NewReader(strings.Repeat("a", 4096))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", big))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("超大请求体应 413，got %d", w.Code)
	}
}

func TestTLSMinVersion(t *testing.T) {
	if got := tlsMinVersion(""); got != tls.VersionTLS12 {
		t.Fatalf("默认应 TLS1.2，got %x", got)
	}
	if got := tlsMinVersion("1.3"); got != tls.VersionTLS13 {
		t.Fatalf("1.3 解析失败，got %x", got)
	}
	if got := tlsMinVersion("garbage"); got != tls.VersionTLS12 {
		t.Fatalf("非法值应回退 TLS1.2，got %x", got)
	}
}

func TestSplitList(t *testing.T) {
	got := splitList(" 10.0.0.1/8 , ,172.16.0.0/12")
	if len(got) != 2 || got[0] != "10.0.0.1/8" || got[1] != "172.16.0.0/12" {
		t.Fatalf("splitList=%v", got)
	}
	if splitList("") != nil {
		t.Fatal("空串应返回 nil")
	}
}
