// Package coherence 承载 v4v6 后缀一致性联动的核心计算与服务（§4.3/§2.1）。
package coherence

import (
	"fmt"
	"net"
	"strings"
)

// Template 映射模板（PG prefix_template 行的最小投影）。
type Template struct {
	ID     string
	Prefix string // 如 "2406::"
	Expr   string // "{v4.hextet4}" | "{v4.hex32}"
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

	var suffix string
	switch t.Expr {
	case "{v4.hextet4}":
		suffix = fmt.Sprintf("%d:%d:%d:%d", o[0], o[1], o[2], o[3])
	case "{v4.hex32}":
		u32 := uint32(o[0])<<24 | uint32(o[1])<<16 | uint32(o[2])<<8 | uint32(o[3])
		suffix = fmt.Sprintf("%x:%x", u32>>16&0xffff, u32&0xffff)
	default:
		return "", fmt.Errorf("unsupported expr %q (B/A 两型外属 CUSTOM，M2 实现)", t.Expr)
	}

	result := strings.TrimSuffix(t.Prefix, "::") + "::" + suffix
	if net.ParseIP(result) == nil {
		return "", fmt.Errorf("mapped result invalid: %q", result)
	}
	return result, nil
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
