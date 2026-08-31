package logquery

import (
	"net/netip"
	"sort"
)

// ipRange 闭区间地址范围（lo/hi 以 16 字节 IPv6 形态参与比较；v4 经 ::ffff 映射）。
type ipRange struct {
	Lo, Hi netip.Addr
}

// mappedV6 归一为 16 字节 IPv6 形态（与 CH IPv6 列存储一致）：
// v4 → ::ffff/96 段映射；纯 v6 → 原样（M2-032 回归修复：此前对 v6 调 As4 会 panic，
// 触发链 v6 池 → dashboard poolTop → DistinctClientIP）。
func mappedV6(a netip.Addr) netip.Addr {
	a = a.Unmap()
	if a.Is4() {
		var b [16]byte
		b[10], b[11] = 0xff, 0xff
		a4 := a.As4()
		copy(b[12:], a4[:])
		return netip.AddrFrom16(b)
	}
	return netip.AddrFrom16(a.As16())
}

// prefixToRange 前缀 → [网络地址, 广播地址]。
func prefixToRange(p netip.Prefix) (ipRange, bool) {
	switch {
	case p.Addr().Is4():
		lo := mappedV6(p.Addr().Unmap())
		return ipRange{Lo: lo, Hi: setHostBits(lo, 96+p.Bits())}, true
	case p.Addr().Is4In6():
		lo := mappedV6(p.Addr().Unmap())
		bits := p.Bits() - 96
		if bits < 0 {
			bits = 128
		}
		return ipRange{Lo: lo, Hi: setHostBits(lo, bits)}, true
	}
	if !p.Addr().Is6() {
		return ipRange{}, false
	}
	lo := netip.AddrFrom16(p.Addr().As16())
	return ipRange{Lo: lo, Hi: setHostBits(lo, p.Bits())}, true
}

// setHostBits 在 16 字节形态上保留前缀 bits、其余置 1 → 范围上界。
func setHostBits(lo netip.Addr, bits int) netip.Addr {
	var b [16]byte
	l16 := lo.As16()
	copy(b[:], l16[:])
	total := 128
	host := total - bits
	for i := 15; i >= 0 && host > 0; i-- {
		fill := min(host, 8)
		b[i] |= byte(0xff >> (8 - fill))
		host -= fill
	}
	return netip.AddrFrom16(b)
}

// filterToRanges 单个 ip 过滤参数（精确地址或 CIDR）→ 合并区间列表。
func filterToRanges(v string) []ipRange {
	if p, err := netip.ParsePrefix(v); err == nil {
		if r, ok := prefixToRange(p); ok {
			return []ipRange{r}
		}
		return nil
	}
	a, err := netip.ParseAddr(v)
	if err != nil {
		return nil
	}
	if a.Is4() || a.Is4In6() {
		m := mappedV6(a)
		return []ipRange{{Lo: m, Hi: m}}
	}
	m := netip.AddrFrom16(a.As16())
	return []ipRange{{Lo: m, Hi: m}}
}

// mergeRanges 排序并合并重叠区间（组织子网常连续，200 CIDR 可收缩为个位数段）。
func mergeRanges(rs []ipRange) []ipRange {
	if len(rs) <= 1 {
		return rs
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].Lo.Compare(rs[j].Lo) < 0 })
	out := make([]ipRange, 0, len(rs))
	cur := rs[0]
	for _, nxt := range rs[1:] {
		if nxt.Lo.Compare(cur.Hi) <= 0 {
			if nxt.Hi.Compare(cur.Hi) > 0 {
				cur.Hi = nxt.Hi
			}
			continue
		}
		out = append(out, cur)
		cur = nxt
	}
	return append(out, cur)
}

// scopeRanges 组织 CIDR 展开合并结果。
func scopeRanges(cidrs []string) []ipRange {
	rs := make([]ipRange, 0, len(cidrs))
	for _, c := range cidrs {
		rs = append(rs, filterToRanges(c)...)
	}
	return mergeRanges(rs)
}
