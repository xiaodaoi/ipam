package backup

import (
	"context"
	"encoding/json"
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
	rows     map[string]int64
	exports  []string
	restored []string
	reset    bool
}

func (f *fakeLogs) MonthRows(context.Context) (map[string]int64, error) {
	out := map[string]int64{}
	if f.rows != nil {
		for m, n := range f.rows {
			out[m] = n
		}
		return out, nil
	}
	for _, m := range f.months {
		out[m] = 1
	}
	return out, nil
}
func (f *fakeLogs) ExportMonth(_ context.Context, month, dir string) (int64, int64, string, error) {
	p := filepath.Join(dir, "logs-"+month+".parquet")
	if err := os.WriteFile(p, []byte("PARQUETPAR1"), 0o640); err != nil {
		return 0, 0, "", err
	}
	f.exports = append(f.exports, month)
	return 1, 11, "hash", nil
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

// seedBackup 构造一份历史成功备份，用于验证复用逻辑。
func seedBackup(t *testing.T, repo Repo, baseDir, id string, months []string, rows map[string]int64, parquet []byte, withMonthRows bool) Record {
	t.Helper()
	dir := filepath.Join(baseDir, id)
	if err := os.MkdirAll(filepath.Join(dir, "clickhouse"), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, m := range months {
		if err := os.WriteFile(filepath.Join(dir, "clickhouse", "logs-"+m+".parquet"), parquet, 0o640); err != nil {
			t.Fatal(err)
		}
	}
	mf := manifest{ID: id, Months: months, ConfigDirs: []string{}}
	if withMonthRows {
		mf.MonthRows = rows
	}
	raw, err := json.Marshal(mf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o640); err != nil {
		t.Fatal(err)
	}
	rec := Record{ID: id, Status: StatusSuccess, Dir: dir, Months: months}
	if err := repo.Create(context.Background(), &rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestBackupReusesUnchangedMonths(t *testing.T) {
	base := t.TempDir()
	repo := NewMemRepo()
	logs := &fakeLogs{rows: map[string]int64{"2026-09": 10, "2026-11": 20}}
	svc := NewService(repo, NewMemSettings(), &fakePg{}, logs, base, nil)

	first, err := svc.Create(context.Background(), KindManual, "")
	if err != nil {
		t.Fatal(err)
	}
	r1 := waitStatus(t, repo, first.ID, StatusSuccess)
	if len(logs.exports) != 2 {
		t.Fatalf("first backup exports=%v, want 2", logs.exports)
	}

	logs.exports = nil
	second, err := svc.Create(context.Background(), KindManual, "")
	if err != nil {
		t.Fatal(err)
	}
	r2 := waitStatus(t, repo, second.ID, StatusSuccess)
	if len(logs.exports) != 0 {
		t.Fatalf("second backup should reuse all months, exports=%v", logs.exports)
	}
	for _, m := range []string{"2026-09", "2026-11"} {
		f1 := filepath.Join(r1.Dir, "clickhouse", "logs-"+m+".parquet")
		f2 := filepath.Join(r2.Dir, "clickhouse", "logs-"+m+".parquet")
		s1, err := os.Stat(f1)
		if err != nil {
			t.Fatal(err)
		}
		s2, err := os.Stat(f2)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(s1, s2) {
			t.Fatalf("%s should be hard-linked", m)
		}
	}
	raw, err := os.ReadFile(filepath.Join(r2.Dir, "manifest.json"))
	if err != nil || !strings.Contains(string(raw), `"monthRows"`) {
		t.Fatalf("manifest should contain monthRows: %s %v", raw, err)
	}
}

func TestBackupReexportsChangedMonth(t *testing.T) {
	base := t.TempDir()
	repo := NewMemRepo()
	logs := &fakeLogs{rows: map[string]int64{"2026-09": 10}}
	svc := NewService(repo, NewMemSettings(), &fakePg{}, logs, base, nil)

	first, err := svc.Create(context.Background(), KindManual, "")
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, repo, first.ID, StatusSuccess)

	logs.rows = map[string]int64{"2026-09": 11}
	logs.exports = nil
	second, err := svc.Create(context.Background(), KindManual, "")
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, repo, second.ID, StatusSuccess)
	if len(logs.exports) != 1 || logs.exports[0] != "2026-09" {
		t.Fatalf("changed month must be re-exported, got %v", logs.exports)
	}
}

func TestBackupDoesNotReuseLegacyManifest(t *testing.T) {
	base := t.TempDir()
	repo := NewMemRepo()
	seedBackup(t, repo, base, "legacy", []string{"2026-09"},
		map[string]int64{"2026-09": 10}, []byte("PARQUETPAR1"), false)

	logs := &fakeLogs{rows: map[string]int64{"2026-09": 10}}
	svc := NewService(repo, NewMemSettings(), &fakePg{}, logs, base, nil)
	rec, err := svc.Create(context.Background(), KindManual, "")
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, repo, rec.ID, StatusSuccess)
	if len(logs.exports) != 1 {
		t.Fatalf("legacy manifest must not be reused, exports=%v", logs.exports)
	}
}

func TestBackupDoesNotReuseTruncatedParquet(t *testing.T) {
	base := t.TempDir()
	repo := NewMemRepo()
	seedBackup(t, repo, base, "truncated", []string{"2026-09"},
		map[string]int64{"2026-09": 10}, []byte("PARTIAL-DATA"), true)

	logs := &fakeLogs{rows: map[string]int64{"2026-09": 10}}
	svc := NewService(repo, NewMemSettings(), &fakePg{}, logs, base, nil)
	rec, err := svc.Create(context.Background(), KindManual, "")
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, repo, rec.ID, StatusSuccess)
	if len(logs.exports) != 1 {
		t.Fatalf("truncated parquet must not be reused, exports=%v", logs.exports)
	}
}
