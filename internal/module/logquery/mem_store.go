package logquery

import (
	"context"
	"net/netip"
	"sort"
	"strings"
	"time"
)

// MemStore 内存实现：PoC 与单测（与 CH 实现共享过滤/排序/游标语义）。
// 数据经 Append 注入；查询为全量过滤（数据量小，无索引）。
type MemStore struct {
	rows []LogRow
}

func NewMemStore() *MemStore { return &MemStore{} }

// Append 追加日志行（按 ts 排序存储，查询时保持确定性）。
func (m *MemStore) Append(rows ...LogRow) {
	m.rows = append(m.rows, rows...)
	sort.SliceStable(m.rows, func(i, j int) bool {
		return m.rows[i].TS.After(m.rows[j].TS)
	})
}

// Query 组合过滤 + 游标分页。
func (m *MemStore) Query(_ context.Context, f LogFilter, scope OrgScope) (Page, error) {
	filtered := make([]LogRow, 0, len(m.rows))
	for _, r := range m.rows {
		if matchLog(r, f, scope) {
			filtered = append(filtered, r)
		}
	}
	// 确定性排序（与 CH ORDER BY 一致）
	sort.SliceStable(filtered, func(i, j int) bool {
		return lessLogDesc(filtered[i], filtered[j])
	})
	total := len(filtered)

	// 游标：仅保留游标之后的行
	if f.Cursor != "" {
		cts, cmac, cdomain := ParseCursor(f.Cursor)
		if !cts.IsZero() {
			kept := filtered[:0]
			for _, r := range filtered {
				if beforeCursor(r, cts, cmac, cdomain) {
					kept = append(kept, r)
				}
			}
			filtered = kept
		}
	}

	pageSize := f.PageSize
	if pageSize <= 0 {
		pageSize = 100
	}
	page := filtered
	next := ""
	if len(filtered) > pageSize {
		page = filtered[:pageSize]
		last := filtered[pageSize-1]
		next = EncodeCursor(last.TS, last.ClientMAC, last.Domain)
	}
	return Page{Items: page, NextCursor: next, Total: total}, nil
}

// Top TopN（物化视图在 CH 侧等价；内存实现直接聚合）。
func (m *MemStore) Top(_ context.Context, q TopQuery, scope OrgScope) ([]TopEntry, int, error) {
	counts := map[string]int{}
	total := 0
	for _, r := range m.rows {
		if !matchTime(r, q.From, q.To) || !matchScope(r, scope) {
			continue
		}
		if q.Type != "" && r.Type != q.Type {
			continue
		}
		if q.Action != "" && r.Action != q.Action {
			continue
		}
		if q.IP != "" && !matchIP(r, q.IP) {
			continue
		}
		key := r.Domain
		if q.By == "client" {
			key = r.ClientMAC
		}
		if key == "" {
			continue
		}
		counts[key]++
		total++
	}
	entries := make([]TopEntry, 0, len(counts))
	for k, c := range counts {
		entries = append(entries, TopEntry{Key: k, Count: c})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Count != entries[j].Count {
			return entries[i].Count > entries[j].Count
		}
		return entries[i].Key < entries[j].Key
	})
	limit := q.Limit
	if limit <= 0 {
		limit = 10
	}
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, total, nil
}

// Qps 时序（按 intervalSec 分桶计数）。
func (m *MemStore) Qps(_ context.Context, q QpsQuery, scope OrgScope) ([]QpsPoint, error) {
	interval := q.IntervalSec
	if interval <= 0 {
		interval = DefaultBucket
	}
	counts := map[int64]int{}
	for _, r := range m.rows {
		if !matchTime(r, q.From, q.To) || !matchScope(r, scope) {
			continue
		}
		if q.Type != "" && r.Type != q.Type {
			continue
		}
		if q.Action != "" && r.Action != q.Action {
			continue
		}
		bucket := r.TS.UTC().Unix() / int64(interval) * int64(interval)
		counts[bucket]++
	}
	points := make([]QpsPoint, 0, len(counts))
	for b, c := range counts {
		points = append(points, QpsPoint{TS: time.Unix(b, 0).UTC(), Count: c})
	}
	sort.Slice(points, func(i, j int) bool { return points[i].TS.Before(points[j].TS) })
	return points, nil
}

// matchLog 行级组合过滤（/logs 语义）。
func matchLog(r LogRow, f LogFilter, scope OrgScope) bool {
	if !matchTime(r, f.From, f.To) {
		return false
	}
	if !matchScope(r, scope) {
		return false
	}
	if f.Type != "" && r.Type != f.Type {
		return false
	}
	if f.MAC != "" && r.ClientMAC != f.MAC {
		return false
	}
	if f.IP != "" && !matchIP(r, f.IP) {
		return false
	}
	if f.Domain != "" && !strings.Contains(strings.ToLower(r.Domain), strings.ToLower(f.Domain)) {
		return false
	}
	if f.Action != "" && r.Action != f.Action {
		return false
	}
	return true
}

func matchTime(r LogRow, from, to time.Time) bool {
	if !from.IsZero() && r.TS.Before(from) {
		return false
	}
	if !to.IsZero() && r.TS.After(to) {
		return false
	}
	return true
}

// matchScope 组织展开命中：client_mac ∈ MACs ∪ (client_ip|sip) ∈ CIDRs。
func matchScope(r LogRow, scope OrgScope) bool {
	if len(scope.CIDRs) == 0 && len(scope.MACs) == 0 {
		return true // 无组织过滤
	}
	for _, mac := range scope.MACs {
		if mac != "" && r.ClientMAC == mac {
			return true
		}
	}
	for _, cidr := range scope.CIDRs {
		if matchAddr(r.ClientIP, cidr) || matchAddr(r.SIP, cidr) {
			return true
		}
	}
	return false
}

// matchIP 精确地址或 CIDR（对 client_ip/sip 任一命中；v4-mapped v6 归一化）。
func matchIP(r LogRow, ipOrCIDR string) bool {
	return matchAddr(r.ClientIP, ipOrCIDR) || matchAddr(r.SIP, ipOrCIDR)
}

// matchAddr addrText 是否命中 ipOrCIDR（精确地址或 CIDR）。
func matchAddr(addrText, ipOrCIDR string) bool {
	if addrText == "" || ipOrCIDR == "" {
		return false
	}
	addr, err := netip.ParseAddr(addrText)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	if p, err := netip.ParsePrefix(ipOrCIDR); err == nil {
		if p.Addr().Is4In6() {
			p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96)
		}
		return p.Contains(addr)
	}
	f, err := netip.ParseAddr(ipOrCIDR)
	if err != nil {
		return false
	}
	return addr == f.Unmap()
}

// lessLogDesc 确定性排序键 (ts DESC, client_mac DESC, domain DESC)。
func lessLogDesc(a, b LogRow) bool {
	if !a.TS.Equal(b.TS) {
		return a.TS.After(b.TS)
	}
	if a.ClientMAC != b.ClientMAC {
		return a.ClientMAC > b.ClientMAC
	}
	return a.Domain > b.Domain
}
