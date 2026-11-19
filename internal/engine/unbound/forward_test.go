package unbound

import (
	"context"
	"strings"
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

// M6-038：候选块含 RPZ 时必须补 respip 到 module-config，否则 checkconf 报
// "RPZ requires the respip module" 使整次 apply 被拒。
func TestCandidateConfText_RPZ补respip(t *testing.T) {
	block := "server:\nrpz:\n  name: \"x.rpz\"\n  zonefile: \"/tmp/x.zone\"\n"
	got := candidateConfText(block)
	if !strings.Contains(got, "module-config: \"respip iterator\"") {
		t.Fatalf("含 RPZ 的候选应补 respip:\n%s", got)
	}
	if !strings.Contains(got, block) {
		t.Fatalf("候选块应原样保留:\n%s", got)
	}
	if strings.Contains(got, "python") {
		t.Fatalf("校验文本不应带 python（校验容器无 python 运行时）:\n%s", got)
	}
}

// 无 RPZ 时保持原样，避免改变既有校验路径。
func TestCandidateConfText_无RPZ不变(t *testing.T) {
	block := "server:\n  forward-zone:\n    name: \".\"\n"
	got := candidateConfText(block)
	if strings.Contains(got, "module-config") {
		t.Fatalf("无 RPZ 不应注入 module-config:\n%s", got)
	}
	if !strings.HasSuffix(got, block) || !strings.HasPrefix(got, "server:") {
		t.Fatalf("候选块应原样保留:\n%s", got)
	}
}
