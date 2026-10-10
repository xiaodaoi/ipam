package logquery

import (
	"testing"
	"time"
)

// 保留期截止：恰为 now 前 180 天（与产品日志保留一致，M5-032），且统一为 UTC。
func TestAuditCutoff(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	want := now.AddDate(0, 0, -auditRetentionDays)
	if got := auditCutoff(now); !got.Equal(want) {
		t.Fatalf("cutoff = %s, want %s", got, want)
	}
	// 非 UTC 输入也必须归一到 UTC（PG timestamptz 比较不受时区影响，但口径需一致）。
	local := now.In(time.FixedZone("CST", 8*3600))
	if got := auditCutoff(local); !got.Equal(want) || got.Location() != time.UTC {
		t.Fatalf("cutoff(%s) = %s (%s), want %s (UTC)", local, got, got.Location(), want)
	}
}

// 清理时点判定：零值（从未清理）立即到期；间隔未满不触发；满 1h 触发。
func TestAuditCleanupDue(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	if !auditCleanupDue(now, time.Time{}) {
		t.Fatal("从未清理应立即到期")
	}
	if auditCleanupDue(now, now.Add(-auditCleanupInterval+time.Minute)) {
		t.Fatal("间隔未满不应触发")
	}
	if !auditCleanupDue(now, now.Add(-auditCleanupInterval)) {
		t.Fatal("满 1h 应触发")
	}
}
