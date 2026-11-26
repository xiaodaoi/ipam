package logmanager

import (
	"testing"
	"time"
)

func TestTTLMatchesDays(t *testing.T) {
	cases := []struct {
		expr string
		days int
		want bool
	}{
		{"toDateTime(ts) + toIntervalDay(90)", 90, true},
		{"toDateTime(ts) + toIntervalDay(90)", 30, false},
		{"toDateTime(ts) + INTERVAL 180 DAY", 180, true},
		{"", 30, false},
	}
	for _, c := range cases {
		if got := ttlMatchesDays(c.expr, c.days); got != c.want {
			t.Errorf("ttlMatchesDays(%q,%d)=%v want %v", c.expr, c.days, got, c.want)
		}
	}
}

func TestPendingArchiveMonthsCompensatesMissedMonths(t *testing.T) {
	now := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	months := []string{"2026-03", "2026-02", "2026-01", "2025-12"}
	archived := map[string]bool{"2025-12": true}
	got := pendingArchiveMonths(months, now, func(m time.Time) bool {
		return archived[m.Format("2006-01")]
	})
	if len(got) != 2 {
		t.Fatalf("got %d months, want 2: %v", len(got), got)
	}
	if got[0].Format("2006-01") != "2026-01" || got[1].Format("2006-01") != "2026-02" {
		t.Fatalf("顺序/内容不符: %v", got)
	}
}
