package unbound

import (
	"context"
	"testing"

	"github.com/xiaodaoi/ipam/internal/module/dns"
)

func TestNormalizeAddr(t *testing.T) {
	if NormalizeAddr("223.5.5.5") != "223.5.5.5:53" {
		t.Fatal("missing port should default 53")
	}
	if NormalizeAddr("1.1.1.1:5353") != "1.1.1.1:5353" {
		t.Fatal("existing port preserved")
	}
}

func TestExecController_缺二进制返回不可用(t *testing.T) {
	c := ExecController{Bin: "/nonexistent/unbound-control"}
	if err := c.SyncForward(context.Background(), []dns.Upstream{{ID: "u1", Addrs: []string{"1.1.1.1"}, Enabled: true}}); err == nil {
		t.Fatal("want error when binary missing")
	}
}

func TestExecController_cmdArgs注入Conf(t *testing.T) {
	e := ExecController{Conf: "/etc/unbound-ctl/unbound.conf"}
	got := e.cmdArgs("forward_add", "corp.local.", "10.0.0.53")
	want := []string{"-c", "/etc/unbound-ctl/unbound.conf", "forward_add", "corp.local.", "10.0.0.53"}
	if len(got) != len(want) {
		t.Fatalf("len=%d want=%d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("[%d] got %s want %s", i, got[i], want[i])
		}
	}
	if e2 := (ExecController{}).cmdArgs("status"); len(e2) != 1 || e2[0] != "status" {
		t.Fatalf("空 Conf 不应注入: %v", e2)
	}
}
