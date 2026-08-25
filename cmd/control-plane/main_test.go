package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
