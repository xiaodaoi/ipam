package main

import (
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
	r := newEngine("test")
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
	r := newEngine("test")
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
	r := newEngine("test")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/dashboard/analytics", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("fallback failed: code=%d ct=%q", w.Code, w.Header().Get("Content-Type"))
	}
}

func TestUnknownAPIRouteReturnsProblem(t *testing.T) {
	r := newEngine("test")
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
