package dns

import (
	"context"
	"strings"
	"testing"
)

func TestRenderSettingsBlock_参数映射(t *testing.T) {
	s := Settings{CacheMinTtl: 0, CacheMaxTtl: 86400, ServeExpired: true,
		RrlEnabled: true, RrlRate: 800, DnssecValidate: true, TcpOnly: false}
	block := RenderSettingsBlock(s)
	for _, want := range []string{
		"cache-min-ttl: 0",
		"cache-max-ttl: 86400",
		"serve-expired: yes",
		"ratelimit: yes",
		"ratelimit-per-ip: 800",
		"val-permissive-mode: no", // DNSSEC 校验开 → 非宽松
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("missing %q in:\n%s", want, block)
		}
	}
	if strings.Contains(block, "do-udp: no") {
		t.Fatal("tcpOnly off must not disable udp")
	}
	tcpOnly := RenderSettingsBlock(Settings{TcpOnly: true})
	if !strings.Contains(tcpOnly, "do-udp: no") {
		t.Fatal("tcpOnly on must disable udp")
	}
}

func TestSettingsService_Update持久化并触发reload(t *testing.T) {
	repo := NewMemSettingsRepo()
	fc := &fakeCtl{}
	svc := NewSettingsService(repo, fc, "/etc/unbound/unbound.conf")

	if err := svc.Update(context.Background(), Settings{CacheMinTtl: 0, CacheMaxTtl: 3600, RrlEnabled: true, RrlRate: 100}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(context.Background())
	if err != nil || got.CacheMaxTtl != 3600 || !got.RrlEnabled || got.RrlRate != 100 {
		t.Fatalf("settings not persisted: %+v err=%v", got, err)
	}
	if fc.reloads == 0 {
		t.Fatal("reload not triggered after update")
	}
}

func TestSettingsService_Flush命令(t *testing.T) {
	svc := NewSettingsService(NewMemSettingsRepo(), &fakeCtl{}, "/tmp/x")
	flushed, cmd, err := svc.Flush(context.Background(), "")
	if err != nil || flushed != "all" || cmd != "unbound-control flush" {
		t.Fatalf("flush all: %s %s err=%v", flushed, cmd, err)
	}
	flushed, cmd, _ = svc.Flush(context.Background(), "corp.local.")
	if flushed != "corp.local." || !strings.Contains(cmd, "flush_zone corp.local.") {
		t.Fatalf("flush zone: %s %s", flushed, cmd)
	}
}
