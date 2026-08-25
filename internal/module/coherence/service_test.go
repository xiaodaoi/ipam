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
