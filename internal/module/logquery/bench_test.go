package logquery

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// 服务层基准：MemStore 上模拟万级日志的组合过滤+分页开销。
// CH 真实数据量基准走 CI 闸⑥ 与 docs/dev 实施说明中的合成灌数流程。
func seedBenchmark(b *testing.B, n int) (*Service, time.Time) {
	b.Helper()
	m := NewMemStore()
	base := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	rows := make([]LogRow, n)
	for i := 0; i < n; i++ {
		rows[i] = LogRow{
			TS:        base.Add(time.Duration(i%3600) * time.Second),
			Type:      [2]string{"dns", "dhcp"}[i%2],
			ClientMAC: fmt.Sprintf("%012x", i%5000),
			ClientIP:  fmt.Sprintf("10.61.%d.%d", i%256, (i/256)%256),
			Domain:    fmt.Sprintf("host%d.corp.local.", i%2000),
			Action:    [3]string{"resolve", "lease_commit", "blocked"}[i%3],
		}
	}
	m.Append(rows...)
	return NewService(m, nil), base
}

func BenchmarkQueryOrgFiltered(b *testing.B) {
	svc, base := seedBenchmark(b, 100_000)
	from := base
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		page, err := svc.Query(context.Background(), LogFilter{
			From: from, Type: "dhcp", PageSize: 100,
		})
		if err != nil || len(page.Items) == 0 {
			b.Fatalf("query: %v", err)
		}
	}
}

func BenchmarkQueryCursorWalk(b *testing.B) {
	svc, base := seedBenchmark(b, 100_000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		cur, from := "", base
		b.StartTimer()
		pages := 0
		for {
			page, err := svc.Query(context.Background(), LogFilter{From: from, Cursor: cur, PageSize: 100})
			if err != nil {
				b.Fatal(err)
			}
			pages++
			if page.NextCursor == "" || pages > 12 {
				break
			}
			cur = page.NextCursor
		}
	}
}

func BenchmarkTopDomains(b *testing.B) {
	svc, base := seedBenchmark(b, 100_000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := svc.Top(context.Background(), TopQuery{
			From: base.Add(-time.Hour), To: base.Add(time.Hour), By: "domain", Limit: 10,
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}
