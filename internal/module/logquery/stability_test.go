package logquery

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
)

// TestTailIncrementalNoLoss（M6-060）：突发超过单批上限时，按游标分页续拉，
// 不丢中间事件且不重复。
func TestTailIncrementalNoLoss(t *testing.T) {
	m := NewMemStore()
	now := time.Now().UTC().Add(-time.Minute)
	const total = 450
	for i := 0; i < total; i++ {
		m.Append(LogRow{
			TS: now.Add(time.Duration(i) * time.Millisecond), Type: "dns",
			ClientMAC: fmt.Sprintf("mac%04d", i), Domain: "x.example.", Action: "resolve",
		})
	}
	s := NewService(m, nil)
	after := ""
	got := 0
	var lastTS time.Time
	for page := 0; page < 10; page++ {
		rows, err := s.Tail(context.Background(), LogFilter{From: now.Add(-time.Hour)}, after, 200)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			if !lastTS.IsZero() && r.TS.Before(lastTS) {
				t.Fatalf("顺序错误: %v < %v", r.TS, lastTS)
			}
			lastTS = r.TS
		}
		got += len(rows)
		if len(rows) < 200 {
			break
		}
		last := rows[len(rows)-1]
		after = EncodeCursor(last.TS, last.ClientMAC, last.Domain)
	}
	if got != total {
		t.Fatalf("tail 收到 %d 条，want %d（丢事件或重复）", got, total)
	}
}

// TestExportLogsStreamsBeyond500（M6-060）：导出不得被 MaxPage=500 静默截断。
func TestExportLogsStreamsBeyond500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := NewMemStore()
	now := time.Now().UTC()
	for i := 0; i < 1200; i++ {
		m.Append(LogRow{
			TS: now.Add(-time.Duration(i) * time.Second), Type: "dns",
			ClientMAC: fmt.Sprintf("mac%04d", i), ClientIP: "10.0.0.1", Domain: "x.example.",
			Action: "resolve",
		})
	}
	h := NewHandler(NewService(m, nil))
	r := gin.New()
	r.GET("/export", func(c *gin.Context) {
		limit := 1000
		h.ExportLogs(c, apigen.ExportLogsParams{From: now.Add(-2 * time.Hour), ExportLimit: &limit})
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/export", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if w.Header().Get("X-Truncated") != "true" {
		t.Fatalf("超过 limit 未标记截断: %v", w.Header())
	}
	body := strings.TrimPrefix(w.Body.String(), "\xEF\xBB\xBF")
	lines := strings.Split(strings.TrimSpace(body), "\n")
	// 1 行表头 + 1000 行数据
	if len(lines) != 1001 {
		t.Fatalf("导出行数=%d，want 1001（表头+1000）", len(lines))
	}
}

// TestQpsIntervalAutoClamp（M6-060）：大窗口 + 1s 分桶应自动放大，点数有上限。
func TestQpsIntervalAutoClamp(t *testing.T) {
	spy := &qpsSpyStore{MemStore: NewMemStore()}
	s := NewService(spy, nil)
	from := time.Now().UTC().Add(-30 * 24 * time.Hour)
	if _, err := s.Qps(context.Background(), QpsQuery{From: from, IntervalSec: 1}); err != nil {
		t.Fatal(err)
	}
	if spy.seen.IntervalSec <= 1 {
		t.Fatalf("interval 未放大: %d", spy.seen.IntervalSec)
	}
}

type qpsSpyStore struct {
	*MemStore
	seen QpsQuery
}

func (s *qpsSpyStore) Qps(ctx context.Context, q QpsQuery, scope OrgScope) ([]QpsPoint, error) {
	s.seen = q
	return s.MemStore.Qps(ctx, q, scope)
}
