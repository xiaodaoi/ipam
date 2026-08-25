package coherence

import (
	"fmt"
	"net"
	"strings"
)

// RRs 一条绑定应产生的四条记录（§4.4：A/AAAA/双向 PTR）。
// key 唯一定位一条 local_data 行，用于差分。
func DesiredRRs(b Binding, zone string) map[string]string {
	if b.IPv6 == "" {
		return nil
	}
	name := FQDN(b, zone)
	ip4 := net.ParseIP(b.IPv4)
	ip6 := net.ParseIP(b.IPv6)
	if ip4 == nil || ip6 == nil || ip4.To4() == nil || ip6.To16() == nil || ip6.To4() != nil {
		return nil
	}
	out := map[string]string{
		name + "|A":    fmt.Sprintf("%s 300 IN A %s", name, b.IPv4),
		name + "|AAAA": fmt.Sprintf("%s 300 IN AAAA %s", name, b.IPv6),
	}
	if r4, err := reverseV4(ip4.To4()); err == nil {
		out[r4+"|PTR"] = fmt.Sprintf("%s 300 IN PTR %s", r4, name)
	}
	if r6, err := reverseV6(ip6.To16()); err == nil {
		out[r6+"|PTR"] = fmt.Sprintf("%s 300 IN PTR %s", r6, name)
	}
	return out
}

// FQDN 主机名规则（§4.4）：option12/FQDN 缺省 host-<mac横线>。
func FQDN(b Binding, zone string) string {
	host := strings.TrimSuffix(b.Hostname, ".")
	if host == "" {
		host = "host-" + strings.ReplaceAll(strings.ToLower(b.MAC), ":", "-")
	}
	if strings.HasSuffix(host, "."+zone) || host == strings.TrimSuffix(zone, ".") {
		return host + "."
	}
	return host + "." + zone
}

func reverseV4(ip4 net.IP) (string, error) {
	o := []byte(ip4)
	return fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa.", o[3], o[2], o[1], o[0]), nil
}

func reverseV6(ip6 net.IP) (string, error) {
	var sb strings.Builder
	for i := len(ip6) - 1; i >= 0; i-- {
		fmt.Fprintf(&sb, "%x.%x.", ip6[i]&0x0f, ip6[i]>>4)
	}
	sb.WriteString("ip6.arpa.")
	return sb.String(), nil
}

// diff 计算期望态与已应用态的增删集合（对账核心，幂等）。
func diff(applied, desired map[string]string) (adds, dels []string) {
	for k, line := range desired {
		if applied[k] != line {
			adds = append(adds, line)
		}
	}
	for k, line := range applied {
		if _, ok := desired[k]; !ok {
			dels = append(dels, line)
		}
	}
	return adds, dels
}
