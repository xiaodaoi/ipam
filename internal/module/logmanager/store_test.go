package logmanager

import (
	"context"
	"testing"
	"time"
)

func TestMemSettingsRepo(t *testing.T) {
	ctx := context.Background()
	repo := NewMemSettingsRepo()

	got, err := repo.Get(ctx)
	if err != nil {
		t.Fatalf("get default: %v", err)
	}
	if got.RetentionDays != 180 || got.ArchiveKeepMonths != 12 || !got.AutoExport {
		t.Fatalf("unexpected defaults: %+v", got)
	}

	want := Settings{RetentionDays: 90, ArchiveKeepMonths: 6, AutoExport: false}
	if err := repo.Save(ctx, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err = repo.Get(ctx)
	if err != nil {
		t.Fatalf("get after save: %v", err)
	}
	if got != want {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", got, want)
	}
}

func TestMemArchiveRepo(t *testing.T) {
	ctx := context.Background()
	repo := NewMemArchiveRepo()
	jan := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	if _, err := repo.Get(ctx, jan); err == nil {
		t.Fatal("expected not-found before upsert")
	}

	a := Archive{Month: jan, RowCount: 100, FileSize: 2048, FilePath: "logs-2026-01.parquet", FileHash: "abc", ExportedBy: "manual"}
	if err := repo.Upsert(ctx, a); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := repo.Upsert(ctx, Archive{Month: feb, RowCount: 200, FileSize: 4096, FilePath: "logs-2026-02.parquet", ExportedBy: "system"}); err != nil {
		t.Fatalf("upsert feb: %v", err)
	}

	got, err := repo.Get(ctx, jan)
	if err != nil {
		t.Fatalf("get jan: %v", err)
	}
	if got.RowCount != 100 || got.FileHash != "abc" {
		t.Fatalf("unexpected archive: %+v", got)
	}

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 archives, got %d", len(list))
	}
	if !list[0].Month.Equal(feb) || !list[1].Month.Equal(jan) {
		t.Fatalf("list not sorted desc: %+v", list)
	}

	if err := repo.Upsert(ctx, Archive{Month: jan, RowCount: 150, FileSize: 3000, FilePath: "logs-2026-01.parquet", ExportedBy: "system"}); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	if list, _ = repo.List(ctx); len(list) != 2 {
		t.Fatalf("upsert must not duplicate, got %d", len(list))
	}

	if err := repo.Delete(ctx, jan); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if list, _ = repo.List(ctx); len(list) != 1 {
		t.Fatalf("want 1 after delete, got %d", len(list))
	}
}

func TestParseMonth(t *testing.T) {
	good, err := parseMonth("2026-03")
	if err != nil {
		t.Fatalf("parse good: %v", err)
	}
	if good.Year() != 2026 || good.Month() != time.March {
		t.Fatalf("unexpected: %v", good)
	}
	for _, bad := range []string{"2026-3", "202603", "2026-13", "../etc", "2026-03-01", ""} {
		if _, err := parseMonth(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

func TestDeriveHTTPAddr(t *testing.T) {
	cases := map[string]string{
		"clickhouse:9000": "clickhouse:8123",
		"clickhouse":      "clickhouse:8123",
		"":                "",
	}
	for in, want := range cases {
		if got := deriveHTTPAddr(in); got != want {
			t.Fatalf("deriveHTTPAddr(%q) = %q, want %q", in, got, want)
		}
	}
}
