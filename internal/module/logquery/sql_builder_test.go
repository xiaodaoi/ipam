package logquery

import (
	"net/netip"
	"strings"
	"testing"
	"time"
)

// buildWhere 子句/参数占位符必须一一对应（BETWEEN 多列展开是错位高发区）。
func assertAligned(t *testing.T, where string, args []any) {
	t.Helper()
	if n := strings.Count(where, "?"); n != len(args) {
		t.Fatalf("占位符 %d != 参数 %d\nwhere=%s", n, len(args), where)
	}
}

func TestBuildWhereAlignment(t *testing.T) {
	from := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	cases := []cond{
		{From: from},
		{From: from, Type: "dhcp"},
		{From: from, MAC: "aa:bb:cc:dd:ee:01"},
		{From: from, IP: "10.61.172.0/24"},
		{From: from, IP: "10.61.172.10"},
		{From: from, IP: "2001:db8::/32"},
		{From: from, Domain: "corp.local", Action: "blocked"},
	}
	for _, c := range cases {
		w, a := buildWhere(c)
		assertAligned(t, w, a)
	}
}

func TestBuildWhereScopeArgsMatchPlaceholders(t *testing.T) {
	from := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	sc := cond{
		From: from,
		Scope: OrgScope{
			MACs:  []string{"aabbccddee01", "", "aabbccddee02", "aabbccddee01"},  // 去重去空 → 2
			CIDRs: []string{"192.168.0.0/16", "192.168.1.0/24", "2001:db8::/32"}, // 前两段包含合并 → 2 段
		},
	}
	w, a := buildWhere(sc)
	assertAligned(t, w, a)

	macN := len(dedupNonEmpty(sc.Scope.MACs))
	rangeN := len(scopeRanges(dedupNonEmpty(sc.Scope.CIDRs)))
	wantMacArgs, wantRangeArgs := macN, rangeN*4 // 每段区间 × 2 列 × 2 端点
	gotMacArgs, gotRangeArgs := 0, 0
	for _, v := range a {
		switch v.(type) {
		case string:
			gotMacArgs++
		case netip.Addr:
			gotRangeArgs++
		}
	}
	if gotMacArgs != wantMacArgs {
		t.Fatalf("MAC 参数 %d != %d", gotMacArgs, wantMacArgs)
	}
	if gotRangeArgs != wantRangeArgs {
		t.Fatalf("区间参数 %d != %d（应为每段 4 个 netip.Addr）", gotRangeArgs, wantRangeArgs)
	}
}

func TestMergeOverlappingCIDRs(t *testing.T) {
	rs := scopeRanges([]string{"10.61.171.0/24", "10.61.172.0/24", "10.61.173.0/24"})
	if len(rs) != 3 { // 相邻但不重叠 → 不合并
		t.Fatalf("want 3 ranges, got %d", len(rs))
	}
	rs = scopeRanges([]string{"192.168.0.0/16", "192.168.1.0/24"})
	if len(rs) != 1 {
		t.Fatalf("包含关系应合并为 1, got %d", len(rs))
	}
	if rs[0].Lo.Compare(mappedV6(netip.MustParseAddr("192.168.0.0"))) != 0 {
		t.Fatalf("v4→v6 映射 lo 异常: %s", rs[0].Lo.String())
	}
	if rs[0].Hi.Compare(mappedV6(netip.MustParseAddr("192.168.255.255"))) != 0 {
		t.Fatalf("v4→v6 映射 hi 异常: %s", rs[0].Hi.String())
	}
}

func TestMappedV6RangeBounds(t *testing.T) {
	r, ok := prefixToRange(netip.MustParsePrefix("10.0.0.0/8"))
	if !ok {
		t.Fatal("v4 前缀应可转区间")
	}
	wantHi := mappedV6(netip.MustParseAddr("10.255.255.255"))
	if r.Hi.Compare(wantHi) != 0 {
		t.Fatalf("/8 hi = %s, want %s", r.Hi.String(), wantHi.String())
	}
	r6, _ := prefixToRange(netip.MustParsePrefix("2001:db8::/32"))
	if r6.Lo.String() != "2001:db8::" || r6.Hi.String() != "2001:db8:ffff:ffff:ffff:ffff:ffff:ffff" {
		t.Fatalf("v6 /32 区间异常: %s..%s", r6.Lo.String(), r6.Hi.String())
	}
}
