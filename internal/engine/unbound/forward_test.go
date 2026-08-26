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
