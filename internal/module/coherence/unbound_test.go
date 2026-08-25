package coherence

import (
	"errors"
	"strings"
	"testing"
)

var b1 = Binding{
	MAC: "aa:bb:cc:dd:ee:01", IPv4: "10.61.172.10", IPv6: "2406::10:61:172:10",
	TemplateID: "t-b", Hostname: "printer",
}

func TestDesiredRRs_四条记录(t *testing.T) {
	got := DesiredRRs(b1, "corp.local.")
	if len(got) != 4 {
		t.Fatalf("want 4 RRs, got %d: %v", len(got), got)
	}
	wantA := "printer.corp.local. 300 IN A 10.61.172.10"
	if got["printer.corp.local.|A"] != wantA {
		t.Fatalf("A line: %q", got["printer.corp.local.|A"])
	}
	// IPv6 反解 nibble：...:17:2:10 → 0.1.7.1... 逐 nibble 反转
	if !strings.Contains(got[keyPTR6(t)], "ip6.arpa.") {
		t.Fatalf("v6 PTR name wrong")
	}
	if !strings.Contains(got["10.172.61.10.in-addr.arpa.|PTR"], "printer.corp.local.") {
		t.Fatalf("v4 PTR line: %v", got)
	}
}

func keyPTR6(t *testing.T) string {
	for k := range DesiredRRs(b1, "corp.local.") {
		if strings.Contains(k, "ip6.arpa.") {
			return k
		}
	}
	t.Fatal("no v6 ptr")
	return ""
}

func TestFQDN_MAC缺省规则(t *testing.T) {
	b := Binding{MAC: "AA:BB:CC:DD:EE:FF"}
	if got := FQDN(b, "corp.local."); got != "host-aa-bb-cc-dd-ee-ff.corp.local." {
		t.Fatalf("got %q", got)
	}
}

func TestDiff_增删差分幂等(t *testing.T) {
	applied := map[string]string{"a.x.|A": "a.x. 300 IN A 1.1.1.1", "gone.y.|A": "gone.y. 300 IN A 2.2.2.2"}
	desired := map[string]string{"a.x.|A": "a.x. 300 IN A 1.1.1.1", "new.z.|AAAA": "new.z. 300 IN AAAA ::3"}

	adds, dels := diff(applied, desired)
	if len(adds) != 1 || !strings.HasPrefix(adds[0], "new.z.") {
		t.Fatalf("adds: %v", adds)
	}
	if len(dels) != 1 || !strings.HasPrefix(dels[0], "gone.y.") {
		t.Fatalf("dels: %v", dels)
	}
}

type fakeCtl struct {
	added, removed []string
	failNext       bool
}

func (f *fakeCtl) Add(line string) error {
	if f.failNext {
		f.failNext = false
		return errors.New("boom")
	}
	f.added = append(f.added, line)
	return nil
}
func (f *fakeCtl) Remove(line string) error {
	f.removed = append(f.removed, line)
	return nil
}

func TestReconciler_失败保留状态待重试(t *testing.T) {
	fc := &fakeCtl{failNext: true}
	r := NewReconciler(fc, "corp.local.")

	if err := r.Sync([]Binding{b1}); err == nil {
		t.Fatal("want first sync fail")
	}
	if err := r.Sync([]Binding{b1}); err != nil {
		t.Fatalf("retry should succeed: %v", err)
	}
	if len(fc.added) == 0 {
		t.Fatal("nothing applied on retry")
	}
	// 幂等：同态重跑零操作
	before := len(fc.added)
	if err := r.Sync([]Binding{b1}); err != nil || len(fc.added) != before {
		t.Fatalf("not idempotent: added=%d", len(fc.added))
	}
}

func TestReconciler_换址产生删除与新增(t *testing.T) {
	fc := &fakeCtl{}
	r := NewReconciler(fc, "corp.local.")
	_ = r.Sync([]Binding{b1})

	b2 := b1
	b2.IPv6 = "2406::aa:bb"
	_ = r.Sync([]Binding{b2})

	joined := strings.Join(fc.removed, "\n")
	if !strings.Contains(joined, "printer") {
		t.Fatalf("old records not removed: %s", joined)
	}
	foundNew := false
	for _, a := range fc.added {
		if strings.Contains(a, "2406::aa:bb") {
			foundNew = true
		}
	}
	if !foundNew {
		t.Fatalf("new AAAA not added: %v", fc.added)
	}
}

func TestExecController_缺二进制返回Unavailable(t *testing.T) {
	e := ExecController{Bin: "/nonexistent/unbound-control"}
	if err := e.Add("x"); !errors.Is(err, ErrUnboundUnavailable) {
		t.Fatalf("err = %v", err)
	}
}
