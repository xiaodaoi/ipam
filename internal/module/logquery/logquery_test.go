package logquery

import (
	"context"
	"errors"
	"testing"
	"time"
)

func mkRow(ts time.Time, typ, mac, ip, domain, action string) LogRow {
	return LogRow{TS: ts, Type: typ, ClientMAC: mac, ClientIP: ip, Domain: domain, Action: action}
}

func base(t *testing.T) (*MemStore, time.Time) {
	t.Helper()
	now := time.Date(2026, 8, 26, 8, 0, 0, 0, time.UTC)
	m := NewMemStore()
	m.Append(
		mkRow(now.Add(-10*time.Minute), "dns", "", "10.61.172.10", "api.corp.local.", "resolve"),
		mkRow(now.Add(-9*time.Minute), "dns", "", "10.61.172.10", "update.corp.local.", "resolve"),
		mkRow(now.Add(-8*time.Minute), "dhcp", "aabbccddee01", "10.61.172.10", "", "lease_commit"),
		mkRow(now.Add(-7*time.Minute), "dns", "", "10.61.172.11", "api.corp.local.", "blocked"),
		mkRow(now.Add(-6*time.Minute), "dhcp", "aabbccddee02", "10.61.172.11", "", "lease_commit"),
		mkRow(now.Add(-5*time.Minute), "dns", "", "10.61.173.20", "bad.example.", "blocked"),
	)
	return m, now
}

func TestQueryFilters(t *testing.T) {
	m, now := base(t)
	s := NewService(m, nil)
	from := now.Add(-30 * time.Minute)

	cases := []struct {
		name string
		f    LogFilter
		want int
	}{
		{"无过滤", LogFilter{From: from}, 6},
		{"type=dns", LogFilter{From: from, Type: "dns"}, 4},
		{"type=dhcp", LogFilter{From: from, Type: "dhcp"}, 2},
		{"mac 过滤", LogFilter{From: from, MAC: "aa:bb:cc:dd:ee:01"}, 1},
		{"ip 精确", LogFilter{From: from, IP: "10.61.172.10"}, 3},
		{"ip CIDR", LogFilter{From: from, IP: "10.61.172.0/24"}, 5},
		{"domain 子串", LogFilter{From: from, Domain: "corp.local"}, 3},
		{"action", LogFilter{From: from, Action: "blocked"}, 2},
		{"组合 type+ip+action", LogFilter{From: from, Type: "dns", IP: "10.61.172.0/24", Action: "blocked"}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page, err := s.Query(context.Background(), tc.f)
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if len(page.Items) != tc.want {
				t.Fatalf("got %d rows, want %d", len(page.Items), tc.want)
			}
			if page.Total != tc.want {
				t.Fatalf("total %d, want %d", page.Total, tc.want)
			}
		})
	}
}

func TestQueryCursorPagination(t *testing.T) {
	m, now := base(t)
	s := NewService(m, nil)
	from := now.Add(-30 * time.Minute)

	// 每页 2 条翻完 6 行，无重复无遗漏
	var seen []string
	cur := ""
	pages := 0
	for {
		page, err := s.Query(context.Background(), LogFilter{From: from, Cursor: cur, PageSize: 2})
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
		for _, r := range page.Items {
			seen = append(seen, r.ClientIP+r.Domain+r.Type)
		}
		pages++
		if page.NextCursor == "" {
			if page.Total != 6 {
				t.Fatalf("final page total %d, want 6", page.Total)
			}
			break
		}
		if len(page.Items) != 2 {
			t.Fatalf("page %d 行数 %d, want 2", pages, len(page.Items))
		}
		cur = page.NextCursor
	}
	if len(seen) != 6 {
		t.Fatalf("翻页共 %d 行, want 6", len(seen))
	}
	// 无重复
	uniq := map[string]bool{}
	for _, s := range seen {
		if uniq[s] {
			t.Fatalf("重复行 %s", s)
		}
		uniq[s] = true
	}
	// 倒序：第一页第一行应为最新
	first, _ := s.Query(context.Background(), LogFilter{From: from, PageSize: 1})
	if !first.Items[0].TS.Equal(now.Add(-5 * time.Minute)) {
		t.Fatalf("首页应最新，got %v", first.Items[0].TS)
	}
}

func TestQueryInvalidCursor(t *testing.T) {
	m, now := base(t)
	s := NewService(m, nil)
	_, err := s.Query(context.Background(), LogFilter{From: now.Add(-30 * time.Minute), Cursor: "garbage"})
	if !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("want ErrInvalidCursor, got %v", err)
	}
}

func TestServiceWindowValidation(t *testing.T) {
	m := NewMemStore()
	s := NewService(m, nil)
	now := time.Now().UTC()

	if _, err := s.Query(context.Background(), LogFilter{}); !errors.Is(err, ErrMissingFrom) {
		t.Fatalf("want ErrMissingFrom, got %v", err)
	}
	if _, err := s.Query(context.Background(), LogFilter{From: now.Add(-40 * 24 * time.Hour), To: now}); !errors.Is(err, ErrWindowTooLarge) {
		t.Fatalf("want ErrWindowTooLarge, got %v", err)
	}
}

func TestTop(t *testing.T) {
	m, now := base(t)
	s := NewService(m, nil)
	from := now.Add(-30 * time.Minute)

	got, total, err := s.Top(context.Background(), TopQuery{From: from, By: "domain", Limit: 3})
	if err != nil {
		t.Fatalf("Top: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3", len(got))
	}
	if got[0].Key != "api.corp.local." || got[0].Count != 2 {
		t.Fatalf("首项应为 api.corp.local.×2, got %+v", got[0])
	}
	if total != 4 { // dns 行数（bad.example./api×2/update）
		t.Fatalf("total %d, want 4", total)
	}

	byClient, _, err := s.Top(context.Background(), TopQuery{From: from, By: "client", Limit: 5})
	if err != nil {
		t.Fatalf("Top client: %v", err)
	}
	if len(byClient) != 2 || byClient[0].Key != "aabbccddee01" {
		t.Fatalf("client TopN 异常: %+v", byClient)
	}
}

func TestQps(t *testing.T) {
	m, now := base(t)
	s := NewService(m, nil)
	from := now.Add(-30 * time.Minute)

	points, err := s.Qps(context.Background(), QpsQuery{From: from, IntervalSec: 3600})
	if err != nil {
		t.Fatalf("Qps: %v", err)
	}
	// 6 行均在同一小时内 → 单桶 6
	if len(points) != 1 || points[0].Count != 6 {
		t.Fatalf("got %+v, want 单桶×6", points)
	}

	fine, _ := s.Qps(context.Background(), QpsQuery{From: from, IntervalSec: 60})
	if len(fine) != 6 {
		t.Fatalf("分钟粒度应有 6 桶, got %d", len(fine))
	}
}

func TestOrgScopeFilter(t *testing.T) {
	m, now := base(t)
	s := NewService(m, nil)
	from := now.Add(-30 * time.Minute)

	// 组织=CIDR 10.61.172.0/24 ∪ MAC aabbccddee02（10.61.172.11 那台）
	scope := OrgScope{CIDRs: []string{"10.61.172.0/24"}, MACs: []string{"aabbccddee02"}}
	page, err := s.store.Query(context.Background(), LogFilter{From: from}, scope)
	if err != nil {
		t.Fatalf("Query scope: %v", err)
	}
	if len(page.Items) != 5 {
		t.Fatalf("CIDR 展开应命中 5 行（排除 10.61.173.20）, got %d", len(page.Items))
	}
}

func TestNormalizeMAC(t *testing.T) {
	cases := map[string]string{
		"aa:bb:cc:dd:ee:01": "aabbccddee01",
		"AA-BB-CC-DD-EE-01": "aabbccddee01",
		"aabbccddee01":      "aabbccddee01",
	}
	for in, want := range cases {
		if got := NormalizeMAC(in); got != want {
			t.Fatalf("NormalizeMAC(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestCursorRoundTrip(t *testing.T) {
	ts := time.Date(2026, 8, 26, 8, 0, 0, 123000000, time.UTC)
	enc := EncodeCursor(ts, "aabbccddee01", "api.corp.local.")
	gotTS, gotMAC, gotDomain := ParseCursor(enc)
	if gotTS != ts || gotMAC != "aabbccddee01" || gotDomain != "api.corp.local." {
		t.Fatalf("roundtrip 失败: %v %s %s", gotTS, gotMAC, gotDomain)
	}
}
