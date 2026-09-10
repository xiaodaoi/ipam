// Package coherence 承载 v4v6 后缀一致性联动的核心计算与服务（§4.3/§2.1）。
package coherence

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// Template 映射模板（PG prefix_template 行的最小投影）。
type Template struct {
	ID     string
	V4Cidr string // 关联的 IPv4 网段（多池对自动匹配依据）
	Prefix string // 如 "2407::"
	Expr   string // "{v4.hextet4}" | "{v4.hex32}"
}

// MatchIPv4Template 按 IPv4 最长前缀匹配模板（多池对：每对 v4/v6 池一条模板，§4.3）。
func MatchIPv4Template(templates []Template, ipv4 string) (Template, error) {
	ip, err := netip.ParseAddr(ipv4)
	if err != nil || !ip.Is4() {
		return Template{}, fmt.Errorf("invalid ipv4 %q", ipv4)
	}
	var best *Template
	bestBits := -1
	for i := range templates {
		p, perr := netip.ParsePrefix(strings.TrimSpace(templates[i].V4Cidr))
		if perr != nil {
			continue
		}
		if p.Contains(ip) && p.Bits() > bestBits {
			best, bestBits = &templates[i], p.Bits()
		}
	}
	if best == nil {
		return Template{}, fmt.Errorf("no template covers %s", ipv4)
	}
	return *best, nil
}

// ApplyTemplate 按 §4.3 规则将 ipv4 映射为 template 前缀下的 IPv6。
// B 型 {v4.hextet4}: 10.61.172.10 → 2406::10:61:172:10
// A 型 {v4.hex32}:   10.61.172.10 → 2406::a3d:ac0a
func ApplyTemplate(t Template, ipv4 string) (string, error) {
	ip := net.ParseIP(ipv4)
	if ip == nil || ip.To4() == nil {
		return "", fmt.Errorf("invalid ipv4 %q", ipv4)
	}
	o := ip.To4()

	// 后缀组（16 位一组）按字节 8 起写入（B 型占满 64-127 位，A 型占 64-95 位）
	var groups [4]uint16
	switch t.Expr {
	case "{v4.hextet4}":
		// B 型十进制镜像：八位组的十进制字面量按十六进制组解释（10 → 0x10）
		for i := 0; i < 4; i++ {
			v, err := strconv.ParseUint(fmt.Sprintf("%d", o[i]), 16, 16)
			if err != nil {
				return "", fmt.Errorf("hextet4 group %d invalid: %w", i, err)
			}
			groups[i] = uint16(v)
		}
	case "{v4.hex32}":
		// A 型 hex32：v4 整体 32 位落在地址末 32 位（bytes 12-15）
		u32 := uint32(o[0])<<24 | uint32(o[1])<<16 | uint32(o[2])<<8 | uint32(o[3])
		groups[2] = uint16(u32 >> 16 & 0xffff)
		groups[3] = uint16(u32 & 0xffff)
	default:
		return "", fmt.Errorf("unsupported expr %q (B/A 两型外属 CUSTOM，M2 实现)", t.Expr)
	}

	var base netip.Addr
	if bits := strings.IndexByte(t.Prefix, '/'); bits >= 0 {
		pr, perr := netip.ParsePrefix(strings.TrimSpace(t.Prefix))
		if perr != nil {
			return "", fmt.Errorf("invalid prefix %q: %w", t.Prefix, perr)
		}
		if pr.Bits() > 112 {
			return "", fmt.Errorf("prefix %q 过长，接口标识不足 16 位", t.Prefix)
		}
		base = pr.Addr()
	} else {
		a, aerr := netip.ParseAddr(strings.TrimSpace(t.Prefix))
		if aerr != nil {
			return "", fmt.Errorf("invalid prefix %q: %w", t.Prefix, aerr)
		}
		base = a
	}
	b := base.As16()
	for i := 0; i < 4; i++ {
		b[8+2*i] = byte(groups[i] >> 8)
		b[8+2*i+1] = byte(groups[i])
	}
	return netip.AddrFrom16(b).String(), nil
}

// NormalizeMAC 归一化任意常见书写为小写冒号格式；非法返回空串。
// 与 C++ 侧 NormalizeMac 行为对齐（快照键一致性前提）。
func NormalizeMAC(raw string) string {
	var b strings.Builder
	b.Grow(17)
	n := 0
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
			if n > 0 && n%2 == 0 {
				b.WriteByte(':')
			}
			b.WriteByte(strings.ToLower(string(c))[0])
			n++
		case c == ':', c == '-', c == '.':
		default:
			return ""
		}
	}
	if n != 12 {
		return ""
	}
	return b.String()
}
