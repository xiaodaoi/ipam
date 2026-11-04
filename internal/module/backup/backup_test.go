package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakePg struct {
	restored string
}

func (f *fakePg) Dump(_ context.Context, dst string) (string, int64, error) {
	if err := os.WriteFile(dst, []byte("PGDUMP"), 0o640); err != nil {
		return "", 0, err
	}
	return "checksum-pg", 6, nil
}
func (f *fakePg) Restore(_ context.Context, src string) error {
	f.restored = src
	return nil
}

type fakeLogs struct {
	months   []string
	exports  []string
	restored []string
	reset    bool
}

func (f *fakeLogs) Months(context.Context) ([]string, error) { return f.months, nil }
func (f *fakeLogs) ExportMonth(_ context.Context, month, dir string) (int64, int64, string, error) {
	p := filepath.Join(dir, "logs-"+month+".parquet")
	if err := os.WriteFile(p, []byte("PARQUET"), 0o640); err != nil {
		return 0, 0, "", err
	}
	f.exports = append(f.exports, month)
	return 1, 7, "hash", nil
}
func (f *fakeLogs) Reset(context.Context) error { f.reset = true; return nil }
func (f *fakeLogs) RestoreMonth(_ context.Context, file string) error {
	f.restored = append(f.restored, file)
	return nil
}

func waitStatus(t *testing.T, repo Repo, id, want string) Record {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r, ok, _ := repo.Get(context.Background(), id)
		if ok && r.Status == want {
			return r
		}
		time.Sleep(20 * time.Millisecond)
	}
	r, _, _ := repo.Get(context.Background(), id)
	t.Fatalf("backup %s status=%s, want %s", id, r.Status, want)
	return r
}

func TestBackupCreateRestoreDelete(t *testing.T) {
	dir := t.TempDir()
	repo := NewMemRepo()
	settings := NewMemSettings()
	pg := &fakePg{}
	logs := &fakeLogs{months: []string{"2026-09", "2026-10"}}
	cfg := t.TempDir()
	if err := os.WriteFile(filepath.Join(cfg, "unbound.conf"), []byte("server:"), 0o640); err != nil {
		t.Fatal(err)
	}
	svc := NewService(repo, settings, pg, logs, dir, []string{cfg})

	rec, err := svc.Create(context.Background(), KindManual, "test")
	if err != nil {
		t.Fatal(err)
	}
	done := waitStatus(t, repo, rec.ID, StatusSuccess)
	if done.Checksum != "checksum-pg" || done.SizeBytes == 0 || len(done.Months) != 2 {
		t.Fatalf("record: %+v", done)
	}
	for _, f := range []string{"postgres.dump", "config.tar.gz", "manifest.json", "clickhouse/logs-2026-09.parquet"} {
		if _, err := os.Stat(filepath.Join(done.Dir, f)); err != nil {
			t.Fatalf("missing %s: %v", f, err)
		}
	}

	restored, err := svc.Restore(context.Background(), rec.ID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !logs.reset || len(logs.restored) != 2 || pg.restored == "" || restored.Status != StatusSuccess {
		t.Fatalf("restore not applied: logs=%+v pg=%+v rec=%+v", logs, pg, restored)
	}

	if err := svc.Delete(context.Background(), rec.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(done.Dir); !os.IsNotExist(err) {
		t.Fatalf("backup dir should be removed: %v", err)
	}
}

func TestBackupCleanupRetention(t *testing.T) {
	dir := t.TempDir()
	repo := NewMemRepo()
	svc := NewService(repo, NewMemSettings(), &fakePg{}, &fakeLogs{}, dir, nil)
	for i := 0; i < 3; i++ {
		r, err := svc.Create(context.Background(), KindManual, "")
		if err != nil {
			t.Fatal(err)
		}
		waitStatus(t, repo, r.ID, StatusSuccess)
	}
	svc.Cleanup(context.Background(), 2)
	list, _ := repo.List(context.Background(), 10)
	if len(list) != 2 {
		t.Fatalf("retention 2, got %d", len(list))
	}
}

// failingLogs 写出半成品后返回错误，用于验证失败清理（M6-028）。
type failingLogs struct{ fakeLogs }

func (f *failingLogs) ExportMonth(_ context.Context, month, dir string) (int64, int64, string, error) {
	if err := os.WriteFile(filepath.Join(dir, "logs-"+month+".parquet"), []byte("PARTIAL"), 0o640); err != nil {
		return 0, 0, "", err
	}
	return 0, 0, "", errors.New("ch http export: simulated failure")
}

// slowLogs 阻塞到 ctx 结束，用于验证总预算超时后状态仍能落库（M6-028）。
type slowLogs struct{ fakeLogs }

func (s *slowLogs) ExportMonth(ctx context.Context, _, _ string) (int64, int64, string, error) {
	<-ctx.Done()
	return 0, 0, "", ctx.Err()
}

func TestBackupExportFailureCleansPartialDir(t *testing.T) {
	dir := t.TempDir()
	repo := NewMemRepo()
	logs := &failingLogs{fakeLogs{months: []string{"2026-10"}}}
	svc := NewService(repo, NewMemSettings(), &fakePg{}, logs, dir, nil)

	rec, err := svc.Create(context.Background(), KindManual, "fail")
	if err != nil {
		t.Fatal(err)
	}
	done := waitStatus(t, repo, rec.ID, StatusFailed)
	if !strings.Contains(done.Error, "clickhouse export 2026-10") {
		t.Fatalf("error=%q", done.Error)
	}
	if done.SizeBytes != 0 || done.FinishedAt == nil {
		t.Fatalf("record=%+v", done)
	}
	if _, err := os.Stat(done.Dir); !os.IsNotExist(err) {
		t.Fatalf("partial backup dir should be removed: %v", err)
	}
}

func TestBackupTimeoutPersistsFailedStatus(t *testing.T) {
	dir := t.TempDir()
	repo := NewMemRepo()
	logs := &slowLogs{fakeLogs{months: []string{"2026-10"}}}
	svc := NewService(repo, NewMemSettings(), &fakePg{}, logs, dir, nil)
	svc.SetTimeout(200 * time.Millisecond)

	rec, err := svc.Create(context.Background(), KindManual, "timeout")
	if err != nil {
		t.Fatal(err)
	}
	// 若 fail() 用已失效的 ctx 写库，记录会卡在 running，这里会超时失败。
	done := waitStatus(t, repo, rec.ID, StatusFailed)
	if !strings.Contains(done.Error, "context deadline exceeded") {
		t.Fatalf("error=%q", done.Error)
	}
	if _, err := os.Stat(done.Dir); !os.IsNotExist(err) {
		t.Fatalf("partial backup dir should be removed: %v", err)
	}
}
