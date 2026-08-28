package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/miekg/dns"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
)

// diagTarget 解析测试台查询目标（§13.4 安全与诊断；M2-014）。
func diagTarget() string {
	if a := os.Getenv("IPAM_DNS_DIAG_ADDR"); a != "" {
		return a
	}
	return "unbound:53"
}

var qtypeMap = map[string]uint16{
	"A": dns.TypeA, "AAAA": dns.TypeAAAA, "CNAME": dns.TypeCNAME, "MX": dns.TypeMX,
	"NS": dns.TypeNS, "TXT": dns.TypeTXT, "PTR": dns.TypePTR, "SOA": dns.TypeSOA,
}

// ptrName IPv4/IPv6 → 反查名；非地址返回空串（PTR 便利形态：直接填 IP）。
func ptrName(name string) string {
	ip, err := netip.ParseAddr(strings.TrimSpace(name))
	if err != nil {
		return ""
	}
	if ip.Is4() {
		b := ip.As4()
		return fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa.", b[3], b[2], b[1], b[0])
	}
	// As16 逐字节双 nibble 逆序展开（零压缩写法天然正确）
	b := ip.As16()
	out := make([]string, 0, 32)
	for i := 15; i >= 0; i-- {
		out = append(out, fmt.Sprintf("%x", b[i]&0xf), fmt.Sprintf("%x", b[i]>>4))
	}
	return strings.Join(out, ".") + ".ip6.arpa."
}

// rrToAnswer RR → 结构化答案（value 按类型格式化，与 dig 输出语义对齐）。
func rrToAnswer(rr dns.RR) apigen.DiagnoseAnswer {
	h := rr.Header()
	ttl := int(h.Ttl)
	ans := apigen.DiagnoseAnswer{Name: h.Name, Type: dns.TypeToString[h.Rrtype], Ttl: &ttl}
	switch v := rr.(type) {
	case *dns.A:
		ans.Value = v.A.String()
	case *dns.AAAA:
		ans.Value = v.AAAA.String()
	case *dns.CNAME:
		ans.Value = v.Target
	case *dns.NS:
		ans.Value = v.Ns
	case *dns.PTR:
		ans.Value = v.Ptr
	case *dns.MX:
		ans.Value = fmt.Sprintf("%d %s", v.Preference, v.Mx)
	case *dns.TXT:
		ans.Value = strings.Join(v.Txt, "")
	case *dns.SOA:
		ans.Value = fmt.Sprintf("%s %s %d %d %d %d %d", v.Ns, v.Mbox, v.Serial, v.Refresh, v.Retry, v.Expire, v.Minttl)
	default:
		ans.Value = rr.String()
	}
	return ans
}

// Diagnose 执行一次实时 DNS 查询（M2-014 测试台）。
// 业务失败（NXDOMAIN/SERVFAIL）与网络失败（Timeout/NetworkError）均结构化返回，不作为错误。
func (s *Service) Diagnose(ctx context.Context, name, qtype string) (apigen.DiagnoseResult, error) {
	qt, ok := qtypeMap[qtype]
	if !ok {
		return apigen.DiagnoseResult{}, fmt.Errorf("unsupported type %q", qtype)
	}
	n := strings.TrimSpace(name)
	if qt == dns.TypePTR {
		if rn := ptrName(n); rn != "" {
			n = rn
		}
	}
	if n == "" || len(n) > 253 {
		return apigen.DiagnoseResult{}, fmt.Errorf("invalid name %q", name)
	}

	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(n), qt)
	m.RecursionDesired = true

	c := new(dns.Client)
	c.Net = "udp"
	ctx2, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	res := apigen.DiagnoseResult{Server: diagTarget(), Answers: []apigen.DiagnoseAnswer{}}
	resp, rtt, err := c.ExchangeContext(ctx2, m, res.Server)
	if err != nil {
		res.Rcode = "NetworkError"
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			res.Rcode = "Timeout"
		}
		return res, nil
	}
	res.Rcode = dns.RcodeToString[resp.Rcode]
	res.RttMs = int(rtt.Milliseconds())
	for _, rr := range resp.Answer {
		res.Answers = append(res.Answers, rrToAnswer(rr))
	}
	return res, nil
}
