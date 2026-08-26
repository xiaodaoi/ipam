package coherence

import (
	"context"
	"testing"

	coherencev1 "github.com/xiaodaoi/ipam/proto/gen/coherence/v1"
)

var tplB = Template{ID: "t-b", Prefix: "2406::", Expr: "{v4.hextet4}"}
var tplA = Template{ID: "t-a", Prefix: "2406::", Expr: "{v4.hex32}"}

func TestApplyTemplate_B型直映(t *testing.T) {
	got, err := ApplyTemplate(tplB, "10.61.172.10")
	if err != nil {
		t.Fatal(err)
	}
	if got != "2406::10:61:172:10" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyTemplate_A型hex32(t *testing.T) {
	got, err := ApplyTemplate(tplA, "10.61.172.10")
	if err != nil {
		t.Fatal(err)
	}
	if got != "2406::a3d:ac0a" {
		t.Fatalf("got %q (want 2406::a3d:ac0a)", got)
	}
}

func TestApplyTemplate_非法输入(t *testing.T) {
	if _, err := ApplyTemplate(tplB, "999.1.1.1"); err == nil {
		t.Fatal("want err for invalid ipv4")
	}
	if _, err := ApplyTemplate(Template{Prefix: "::", Expr: "{v4.custom}"}, "1.2.3.4"); err == nil {
		t.Fatal("want err for CUSTOM expr(M2)")
	}
}

func newSvc() (*Service, *MemStore) {
	s := NewMemStore()
	lookup := func(id string) (Template, bool) {
		switch id {
		case "t-b":
			return tplB, true
		case "t-a":
			return tplA, true
		}
		return Template{}, false
	}
	return NewService(s, lookup), s
}

func TestResolve_NoneThenComputedThenCache(t *testing.T) {
	svc, store := newSvc()
	ctx := context.Background()

	r1, _ := svc.ResolveBinding(ctx, &coherencev1.ResolveRequest{Mac: "aa:bb:cc:dd:ee:01"})
	if r1.Hit || r1.Source != coherencev1.ResolveResponse_NONE {
		t.Fatalf("expect NONE, got %+v", r1)
	}

	store.Put(Binding{MAC: "aa:bb:cc:dd:ee:01", IPv4: "10.61.172.10", TemplateID: "t-b"})
	r2, _ := svc.ResolveBinding(ctx, &coherencev1.ResolveRequest{Mac: "aa:bb:cc:dd:ee:01"})
	if !r2.Hit || r2.Source != coherencev1.ResolveResponse_COMPUTED || r2.Ipv6 != "2406::10:61:172:10" {
		t.Fatalf("expect COMPUTED 2406::10:61:172:10, got %+v", r2)
	}

	r3, _ := svc.ResolveBinding(ctx, &coherencev1.ResolveRequest{Mac: "aa:bb:cc:dd:ee:01"})
	if r3.Source != coherencev1.ResolveResponse_CACHE || r3.Ipv6 != "2406::10:61:172:10" {
		t.Fatalf("expect CACHE hit, got %+v", r3)
	}
}

func TestReportLease_Lifecycle(t *testing.T) {
	svc, store := newSvc()
	ctx := context.Background()

	ack, _ := svc.ReportLease(ctx, &coherencev1.LeaseReport{
		Mac: "aa:bb:cc:dd:ee:02", Event: coherencev1.LeaseEvent_COMMIT,
		Ipv4: "10.0.0.5", Ipv6: "2406::5",
	})
	if !ack.Ok {
		t.Fatalf("commit ack not ok: %+v", ack)
	}
	if b, ok := store.Get("aa:bb:cc:dd:ee:02"); !ok || b.IPv6 != "2406::5" {
		t.Fatalf("commit not stored: %+v %+v", b, ok)
	}

	if _, _ = svc.ReportLease(ctx, &coherencev1.LeaseReport{
		Mac: "aa:bb:cc:dd:ee:02", Event: coherencev1.LeaseEvent_RELEASE,
	}); store.Has("aa:bb:cc:dd:ee:02") {
		t.Fatal("release should delete binding")
	}
}

func TestNormalizeMAC_与Cpp侧对齐(t *testing.T) {
	cases := map[string]string{
		"AA-BB-CC-DD-EE-FF": "aa:bb:cc:dd:ee:ff",
		"aabb.ccdd.eeff":    "aa:bb:cc:dd:ee:ff",
		"AABBCCDDEEFF":      "aa:bb:cc:dd:ee:ff",
		"aa:bb:cc:dd:ee:ff": "aa:bb:cc:dd:ee:ff",
		"":                  "",
		"zz:bb":             "",
		"aa:bb:cc":          "",
	}
	for in, want := range cases {
		if got := NormalizeMAC(in); got != want {
			t.Fatalf("NormalizeMAC(%q)=%q want %q", in, got, want)
		}
	}
}

func TestMatchIPv4Template_多池对最长前缀(t *testing.T) {
	templates := []Template{
		{ID: "t-1", V4Cidr: "192.168.0.0/23", Prefix: "2407::", Expr: "{v4.hextet4}"},
		{ID: "t-2", V4Cidr: "172.16.248.0/24", Prefix: "2409::", Expr: "{v4.hextet4}"},
	}
	// 用户场景：192.168.0.0/23 ↔ 2407::，192.168.0.10 → 2407::192:168:0:10
	tpl, err := MatchIPv4Template(templates, "192.168.0.10")
	if err != nil || tpl.ID != "t-1" {
		t.Fatalf("expect t-1, got %+v err=%v", tpl, err)
	}
	ip6, err := ApplyTemplate(tpl, "192.168.0.10")
	if err != nil || ip6 != "2407::192:168:0:10" {
		t.Fatalf("mapping: %s err=%v", ip6, err)
	}
	// 172.16.248.0/24 ↔ 2409:: 独立成对
	tpl2, _ := MatchIPv4Template(templates, "172.16.248.55")
	if tpl2.ID != "t-2" {
		t.Fatalf("expect t-2, got %+v", tpl2)
	}
	ip62, _ := ApplyTemplate(tpl2, "172.16.248.55")
	if ip62 != "2409::172:16:248:55" {
		t.Fatalf("mapping2: %s", ip62)
	}
	// 无覆盖网段
	if _, err := MatchIPv4Template(templates, "10.0.0.1"); err == nil {
		t.Fatal("want error for unmapped v4")
	}
}

func TestResolve_自动选模板多池对(t *testing.T) {
	svc, store := newSvc()
	store.Put(Binding{MAC: "aa:bb:cc:dd:ee:01", IPv4: "172.16.248.55", TemplateID: ""})
	svc.SetTemplateAll(func() []Template {
		return []Template{
			{ID: "t-1", V4Cidr: "192.168.0.0/23", Prefix: "2407::", Expr: "{v4.hextet4}"},
			{ID: "t-2", V4Cidr: "172.16.248.0/24", Prefix: "2409::", Expr: "{v4.hextet4}"},
		}
	})
	r, _ := svc.ResolveBinding(context.Background(), &coherencev1.ResolveRequest{Mac: "aa:bb:cc:dd:ee:01"})
	if !r.Hit || r.Ipv6 != "2409::172:16:248:55" || r.TemplateId != "t-2" {
		t.Fatalf("auto-template resolve: %+v", r)
	}
}
