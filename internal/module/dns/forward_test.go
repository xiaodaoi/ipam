package dns

import (
	"context"
	"testing"
)

func TestMatchDomain_最长后缀优先(t *testing.T) {
	rules := []ForwardRule{
		{Domain: ".", Enabled: true},
		{Domain: "corp.local.", Enabled: true},
		{Domain: "dev.corp.local.", Enabled: false}, // 禁用不参与
	}
	if got := MatchDomain(rules, "www.example.com"); got == nil || got.Domain != "." {
		t.Fatalf("default match: %+v", got)
	}
	if got := MatchDomain(rules, "host.corp.local."); got == nil || got.Domain != "corp.local." {
		t.Fatalf("corp match: %+v", got)
	}
	if got := MatchDomain(rules, "x.dev.corp.local."); got != nil && got.Domain == "dev.corp.local." {
		t.Fatalf("disabled rule must not match: %+v", got)
	}
	// 大小写与尾点容错
	if got := MatchDomain(rules, "WWW.CORP.LOCAL"); got == nil || got.Domain != "corp.local." {
		t.Fatalf("case-insensitive match: %+v", got)
	}
}

func TestBuildForwardCommands(t *testing.T) {
	ups := []Upstream{
		{ID: "u1", Name: "内网", Addrs: []string{"10.0.0.53"}, Enabled: true},
		{ID: "u2", Name: "阿里", Addrs: []string{"223.5.5.5", "1.1.1.1:5353"}, Enabled: true},
	}
	rules := []ForwardRule{
		{Domain: "corp.local.", UpstreamIDs: []string{"u1"}, Enabled: true},
		{Domain: ".", UpstreamIDs: []string{"u2"}, Enabled: true},
		{Domain: "disabled.x", UpstreamIDs: []string{"u2"}, Enabled: false},
	}
	cmds := buildForwardCommands(rules, ups)
	if len(cmds) != 2 {
		t.Fatalf("want 2 commands, got %d: %v", len(cmds), cmds)
	}
	if cmds[0] != "forward_add corp.local. 10.0.0.53" {
		t.Fatalf("corp cmd: %s", cmds[0])
	}
}

func TestForwardService_CreateDryRun不落库(t *testing.T) {
	upRepo := NewMemUpstreamRepo()
	u, _ := upRepo.Create(context.Background(), Upstream{Name: "a", Addrs: []string{"10.0.0.53"}, Enabled: true})
	frRepo := NewMemForwardRuleRepo()
	svc := NewForwardService(frRepo, upRepo, &fakeCtl{})

	saved, cmds, err := svc.Create(context.Background(), ForwardRule{Domain: "corp.local.", UpstreamIDs: []string{u.ID}, Enabled: true}, true)
	if err != nil {
		t.Fatal(err)
	}
	_ = saved
	if len(cmds) != 1 {
		t.Fatalf("dryrun cmds: %v", cmds)
	}
	list, _ := frRepo.List(context.Background())
	if len(list) != 0 {
		t.Fatal("dryRun must not persist")
	}
}

func TestForwardService_创建后触发下发(t *testing.T) {
	upRepo := NewMemUpstreamRepo()
	u, _ := upRepo.Create(context.Background(), Upstream{Name: "a", Addrs: []string{"10.0.0.53"}, Enabled: true})
	fc := &fakeCtl{}
	svc := NewForwardService(NewMemForwardRuleRepo(), upRepo, fc)

	if _, _, err := svc.Create(context.Background(), ForwardRule{Domain: "corp.local.", UpstreamIDs: []string{u.ID}, Enabled: true}, false); err != nil {
		t.Fatal(err)
	}
	if fc.ruleSyncs != 1 {
		t.Fatalf("rule sync not triggered: %d", fc.ruleSyncs)
	}
}
