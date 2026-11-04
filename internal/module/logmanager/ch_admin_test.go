package logmanager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestChAdmin(t *testing.T, srv *httptest.Server, timeout time.Duration) *ChAdmin {
	t.Helper()
	return &ChAdmin{
		db:            "ipam",
		httpAddr:      strings.TrimPrefix(srv.URL, "http://"),
		user:          "ipam",
		password:      "secret",
		exportTimeout: timeout,
	}
}

func TestExportRange_Success(t *testing.T) {
	const payload = "PARQUET-BYTESPAR1"
	var gotQuery string
	var gotUser, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("max_execution_time")
		gotUser = r.Header.Get("X-ClickHouse-User")
		gotKey = r.Header.Get("X-ClickHouse-Key")
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	dir := t.TempDir()
	a := newTestChAdmin(t, srv, 10*time.Minute)
	rows, size, sum, err := a.exportRange(context.Background(), "2026-10",
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), dir, 42)
	if err != nil {
		t.Fatalf("exportRange: %v", err)
	}
	if rows != 42 || size != int64(len(payload)) {
		t.Fatalf("rows=%d size=%d", rows, size)
	}
	want := sha256.Sum256([]byte(payload))
	if sum != hex.EncodeToString(want[:]) {
		t.Fatalf("checksum=%s", sum)
	}
	if gotUser != "ipam" || gotKey != "secret" {
		t.Fatalf("auth headers: user=%q key=%q", gotUser, gotKey)
	}
	// 服务端预算 = 客户端预算 - 30s（保证先返回 ClickHouse 的明确超时错误）。
	if gotQuery != "570" {
		t.Fatalf("max_execution_time=%q, want 570", gotQuery)
	}
	got, err := os.ReadFile(filepath.Join(dir, "logs-2026-10.parquet"))
	if err != nil || string(got) != payload {
		t.Fatalf("file: %q %v", got, err)
	}
}

func TestExportRange_ServerErrorRemovesPartialFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom: merge failed", http.StatusInternalServerError)
	}))
	defer srv.Close()

	dir := t.TempDir()
	a := newTestChAdmin(t, srv, time.Minute)
	_, _, _, err := a.exportRange(context.Background(), "2026-10",
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), dir, 1)
	if err == nil || !strings.Contains(err.Error(), "status 500") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err=%v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "logs-2026-10.parquet")); !os.IsNotExist(statErr) {
		t.Fatalf("partial file should be removed: %v", statErr)
	}
}

func TestExportRange_TimeoutRemovesPartialFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	a := newTestChAdmin(t, srv, 150*time.Millisecond)
	start := time.Now()
	_, _, _, err := a.exportRange(context.Background(), "2026-10",
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), dir, 1)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "超时") || !strings.Contains(err.Error(), "IPAM_CH_EXPORT_TIMEOUT") {
		t.Fatalf("err=%v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("timeout not honored: %s", elapsed)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "logs-2026-10.parquet")); !os.IsNotExist(statErr) {
		t.Fatalf("partial file should be removed: %v", statErr)
	}
}

func TestExportRange_TruncatedParquetRemoved(t *testing.T) {
	// ClickHouse 流式响应中途超时时仍返回 200，错误文本追加在响应体末尾。
	// 这里模拟「半截数据 + Code: 159」的响应，导出必须判为失败并删除半成品。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("PARTIAL-DATA\nCode: 159. DB::Exception: Timeout exceeded: elapsed 10.2 seconds, maximum: 10\n"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	a := newTestChAdmin(t, srv, time.Minute)
	_, _, _, err := a.exportRange(context.Background(), "2026-10",
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), dir, 1)
	if err == nil {
		t.Fatal("expected incomplete parquet error")
	}
	if !strings.Contains(err.Error(), "不完整") || !strings.Contains(err.Error(), "Code: 159") {
		t.Fatalf("err=%v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "logs-2026-10.parquet")); !os.IsNotExist(statErr) {
		t.Fatalf("partial file should be removed: %v", statErr)
	}
}

func TestOpenChAdmin_ExportTimeoutDefault(t *testing.T) {
	a, err := OpenChAdmin(ChConfig{Addr: "127.0.0.1:9000"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if a.exportTimeout != DefaultExportTimeout {
		t.Fatalf("timeout=%s, want %s", a.exportTimeout, DefaultExportTimeout)
	}
	a2, err := OpenChAdmin(ChConfig{Addr: "127.0.0.1:9000", ExportTimeout: 5 * time.Minute})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if a2.exportTimeout != 5*time.Minute {
		t.Fatalf("timeout=%s, want 5m", a2.exportTimeout)
	}
}
