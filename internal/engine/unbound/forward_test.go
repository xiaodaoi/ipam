package unbound

import (
	"context"
	"os"
	"path/filepath"
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

// M6-041：默认池包含所有 enabled+is_default 上游，地址去重后一次 forward_add。
func TestExecController_SyncForward默认池多地址(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "args.txt")
	bin := filepath.Join(dir, "fake-unbound-control")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$FAKE_OUT\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_OUT", out)

	c := ExecController{Bin: bin}
	ups := []dns.Upstream{
		{ID: "u1", Addrs: []string{"8.8.8.8"}, Enabled: true}, // 非默认，忽略
		{ID: "u2", Addrs: []string{"223.5.5.5", "114.114.114.114:53"}, Enabled: true, IsDefault: true},
		{ID: "u3", Addrs: []string{"223.5.5.5:53", "119.29.29.29"}, Enabled: true, IsDefault: true},
		{ID: "u4", Addrs: []string{"1.1.1.1"}, Enabled: false, IsDefault: true}, // 停用，忽略
	}
	if err := c.SyncForward(context.Background(), ups); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(b)), "\n")
	want := []string{"forward_add", ".", "223.5.5.5@53", "114.114.114.114@53", "119.29.29.29@53"}
	if len(got) != len(want) {
		t.Fatalf("args=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("[%d] got=%q want=%q (all=%v)", i, got[i], want[i], got)
		}
	}
}
