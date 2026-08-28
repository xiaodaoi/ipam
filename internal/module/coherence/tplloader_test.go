package coherence

import "testing"

func TestProjectTemplateNormalizesPrefix(t *testing.T) {
	cases := []struct {
		in   tplRow
		want Template
		ok   bool
	}{
		{tplRow{ID: "t1", V4Cidr: "192.168.0.0/24", V6Pre: "2407::/64", Expr: "{v4.hextet4}"},
			Template{ID: "t1", V4Cidr: "192.168.0.0/24", Prefix: "2407::", Expr: "{v4.hextet4}"}, true},
		{tplRow{ID: "t2", V4Cidr: "10.0.0.0/8", V6Pre: "2406::", Expr: "{v4.hex32}"},
			Template{ID: "t2", V4Cidr: "10.0.0.0/8", Prefix: "2406::", Expr: "{v4.hex32}"}, true},
		{tplRow{ID: "bad", V4Cidr: "192.168.0.0/24", V6Pre: "not-a-prefix", Expr: "{v4.hex32}"}, Template{}, false},
	}
	for _, c := range cases {
		got, ok := projectTemplate(c.in)
		if ok != c.ok {
			t.Fatalf("%s: ok=%v want %v", c.in.ID, ok, c.ok)
		}
		if ok && got != c.want {
			t.Fatalf("%s: got %+v want %+v", c.in.ID, got, c.want)
		}
	}
}

func TestProjectTemplateOutputApplyable(t *testing.T) {
	tpl, ok := projectTemplate(tplRow{ID: "x", V4Cidr: "192.168.0.0/24", V6Pre: "2407::/64", Expr: "{v4.hextet4}"})
	if !ok {
		t.Fatal("project failed")
	}
	ip6, err := ApplyTemplate(tpl, "192.168.0.10")
	if err != nil || ip6 != "2407::192:168:0:10" {
		t.Fatalf("apply: %q err=%v", ip6, err)
	}
}

func TestTplLoaderLookupCache(t *testing.T) {
	l := NewTplLoader(nil)
	l.mu.Lock()
	l.tpls = []Template{{ID: "a", V4Cidr: "10.0.0.0/8", Prefix: "2406::", Expr: "{v4.hextet4}"}}
	l.mu.Unlock()
	if got, ok := l.Lookup("a"); !ok || got.Prefix != "2406::" {
		t.Fatalf("lookup: %+v ok=%v", got, ok)
	}
	if _, ok := l.Lookup("missing"); ok {
		t.Fatal("missing should not hit")
	}
	all := l.All()
	all[0].Prefix = "tampered"
	if l.All()[0].Prefix == "tampered" {
		t.Fatal("All must return a copy")
	}
}

func TestMatchIPv4TemplateLongestPrefixWins(t *testing.T) {
	tpls := []Template{
		{ID: "wide", V4Cidr: "10.0.0.0/8", Prefix: "2406::", Expr: "{v4.hextet4}"},
		{ID: "narrow", V4Cidr: "10.61.0.0/16", Prefix: "2407::", Expr: "{v4.hextet4}"},
	}
	got, err := MatchIPv4Template(tpls, "10.61.172.10")
	if err != nil || got.ID != "narrow" {
		t.Fatalf("longest-prefix: %+v err=%v", got, err)
	}
}
