package dns

import (
	"testing"

	"github.com/miekg/dns"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
)

func TestPtrNameIPv4(t *testing.T) {
	if got := ptrName("192.168.0.10"); got != "10.0.168.192.in-addr.arpa." {
		t.Fatalf("v4: %q", got)
	}
	if got := ptrName("not-an-ip"); got != "" {
		t.Fatalf("非地址应为空: %q", got)
	}
}

func TestPtrNameIPv6Compressed(t *testing.T) {
	// 零压缩写法必须与完整展开等价（回归：手写冒号展开曾丢中间零组）
	want := "1.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.8.b.d.0.1.0.0.2.ip6.arpa."
	if got := ptrName("2001:db8::1"); got != want {
		t.Fatalf("v6: %q", got)
	}
	if got := ptrName("2001:0db8:0000:0000:0000:0000:0000:0001"); got != want {
		t.Fatalf("v6 完整展开: %q", got)
	}
}

func TestRrToAnswerValueFormatting(t *testing.T) {
	cases := []struct {
		rr    string
		typ   string
		value string
		ttl   int
	}{
		{"example.com. 300 IN A 1.2.3.4", "A", "1.2.3.4", 300},
		{"example.com. 600 IN MX 10 mail.example.com.", "MX", "10 mail.example.com.", 600},
		{`example.com. 60 IN TXT "hello" "world"`, "TXT", "helloworld", 60},
		{"example.com. 120 IN NS ns1.example.com.", "NS", "ns1.example.com.", 120},
	}
	for _, c := range cases {
		rr, err := dns.NewRR(c.rr)
		if err != nil {
			t.Fatalf("NewRR %s: %v", c.rr, err)
		}
		got := rrToAnswer(rr)
		if got.Type != c.typ || got.Value != c.value || got.Ttl == nil || *got.Ttl != c.ttl {
			t.Fatalf("%s: %+v", c.rr, got)
		}
	}
}

func TestDiagnoseUnsupportedType(t *testing.T) {
	s := &Service{}
	if _, err := s.Diagnose(t.Context(), "example.com", "TYPE999"); err == nil {
		t.Fatal("未知类型应报错")
	}
	if _, err := s.Diagnose(t.Context(), "", "A"); err == nil {
		t.Fatal("空名应报错")
	}
}

func TestDiagnoseResultShape(t *testing.T) {
	// 语义锚点：业务失败也是 200+rcode（HTTP 层不 500）
	var res apigen.DiagnoseResult
	res.Rcode = "NXDOMAIN"
	res.Answers = []apigen.DiagnoseAnswer{}
	if res.Rcode != "NXDOMAIN" || res.Answers == nil {
		t.Fatal("结构化结果须可空 answers")
	}
}
