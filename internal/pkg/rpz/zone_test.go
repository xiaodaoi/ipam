package rpz

import (
	"strings"
	"testing"
)

func TestRender_动作映射与通配(t *testing.T) {
	text, n := Render("intel.custom.rpz", 42, []Record{
		{Name: "bad.example", Action: ActionNXDomain},
		{Name: "*.wild.example", Action: ActionNXDomain},
		{Name: "drop.example", Action: ActionDrop},
		{Name: "tcp.example", Action: ActionTCPOnly},
		{Name: "page.example", Action: ActionRedirect, IP: "10.0.0.9"},
		{Name: "*.page2.example", Action: ActionRedirect, IP: "10.0.0.9"},
		{Name: "redirect-without-ip.example", Action: ActionRedirect},
	})
	if n != 7 {
		t.Fatalf("want 7 owners, got %d", n)
	}
	cases := []string{
		"bad.example CNAME .",
		"wild.example CNAME .",
		"*.wild.example CNAME .",
		"drop.example CNAME rpz-drop.",
		"tcp.example CNAME rpz-tcp-only.",
		"page.example A 10.0.0.9",
		"page2.example A 10.0.0.9",
		"*.page2.example A 10.0.0.9",
		"redirect-without-ip.example CNAME .", // 无提示页 IP 退化为 NXDOMAIN
	}
	for _, want := range cases {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in zone:\n%s", want, text)
		}
	}
	if !strings.Contains(text, "SOA localhost. hostmaster.localhost. 42 ") {
		t.Fatalf("serial not rendered:\n%s", text)
	}
	if !strings.Contains(text, "$ORIGIN intel.custom.rpz.") {
		t.Fatalf("origin not rendered:\n%s", text)
	}
}

func TestRender_去重优先级与确定性(t *testing.T) {
	in := []Record{
		{Name: "same.example", Action: ActionNXDomain},
		{Name: "SAME.example.", Action: ActionRedirect, IP: "10.1.1.1"},
		{Name: "other.example", Action: ActionDrop},
		{Name: "*.same.example", Action: ActionPassthru},
	}
	a, n := Render("z.rpz", 7, in)
	b, _ := Render("z.rpz", 7, []Record{
		{Name: "*.same.example", Action: ActionPassthru},
		{Name: "other.example", Action: ActionDrop},
		{Name: "same.example", Action: ActionRedirect, IP: "10.1.1.1"},
	})
	if a != b {
		t.Fatalf("render not deterministic:\n--- a ---\n%s\n--- b ---\n%s", a, b)
	}
	if n != 2 {
		t.Fatalf("want 2 owners, got %d", n)
	}
	// passthru 优先级最高，且 same.example 同时输出通配
	if !strings.Contains(a, "same.example CNAME rpz-passthru.") {
		t.Fatalf("passthru should win:\n%s", a)
	}
	if !strings.Contains(a, "*.same.example CNAME rpz-passthru.") {
		t.Fatalf("wildcard should be emitted:\n%s", a)
	}
}

func TestRender_丢弃非法名(t *testing.T) {
	text, n := Render("z.rpz", 1, []Record{
		{Name: "", Action: ActionNXDomain},
		{Name: "localhost", Action: ActionNXDomain},        // 无点
		{Name: "bad name.example", Action: ActionNXDomain}, // 空格
		{Name: "ok.example", Action: ActionNXDomain},
	})
	if n != 1 {
		t.Fatalf("want 1 valid owner, got %d:\n%s", n, text)
	}
	if !strings.Contains(text, "ok.example CNAME .") {
		t.Fatalf("valid record missing:\n%s", text)
	}
}

func TestParseAuthZones(t *testing.T) {
	out := "wl.whitelist.rpz.\tserial 5\t since 1794998065 2026-11-18T10:34:25\n" +
		"intel.hot.rpz.\tserial 11\t since 1794998066 2026-11-18T10:34:26\n" +
		"intel.broad.rpz.\texpired\n" +
		"\n"
	zones := ParseAuthZones(out)
	if len(zones) != 3 {
		t.Fatalf("want 3 zones, got %d: %+v", len(zones), zones)
	}
	if zones[0].Name != "wl.whitelist.rpz" || zones[0].Serial != 5 {
		t.Fatalf("bad parse: %+v", zones[0])
	}
	if zones[2].State != "expired" || zones[2].Serial != 0 {
		t.Fatalf("expired zone mis-parsed: %+v", zones[2])
	}
	m := SerialMap(zones)
	if m["intel.hot.rpz"] != 11 || len(m) != 2 {
		t.Fatalf("serial map wrong: %+v", m)
	}
}

func TestOwners与Diff(t *testing.T) {
	a := Owners(`$TTL 300
$ORIGIN z.rpz.
@ SOA localhost. hostmaster.localhost. 1 3600 600 86400 300
@ NS localhost.
one.example CNAME .
*.wild.example CNAME .
`)
	if len(a) != 1 {
		t.Fatalf("应只取非通配 owner，实际 %+v", a)
	}
	b := Owners(`$TTL 300
$ORIGIN z.rpz.
@ SOA localhost. hostmaster.localhost. 1 3600 600 86400 300
@ NS localhost.
one.example CNAME .
two.example A 10.0.0.9
`)
	added, removed := OwnerDiff(a, b)
	if len(added) != 1 || added[0] != "two.example" {
		t.Fatalf("added=%v", added)
	}
	if len(removed) != 0 {
		t.Fatalf("removed=%v", removed)
	}
	added, removed = OwnerDiff(b, a)
	if len(added) != 0 || len(removed) != 1 || removed[0] != "two.example" {
		t.Fatalf("反向 diff 不符 added=%v removed=%v", added, removed)
	}
}
