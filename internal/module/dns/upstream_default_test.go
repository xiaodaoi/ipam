package dns

import (
	"errors"
	"testing"
)

// M6-041：默认公网出口池选择。
func TestDefaultForwardUpstreams_多默认聚合(t *testing.T) {
	ups := []Upstream{
		{ID: "u1", Addrs: []string{"1.1.1.1"}, Enabled: true},
		{ID: "u2", Addrs: []string{"223.5.5.5"}, Enabled: true, IsDefault: true},
		{ID: "u3", Addrs: []string{"10.0.0.53"}, Enabled: true, IsDefault: true}, // 内部标记不应出现（写入时校验），但选择层只按标记
		{ID: "u4", Addrs: []string{"119.29.29.29"}, Enabled: false, IsDefault: true},
	}
	got := DefaultForwardUpstreams(ups)
	if len(got) != 2 || got[0].ID != "u2" || got[1].ID != "u3" {
		t.Fatalf("expect enabled defaults u2,u3, got %+v", got)
	}
}

// 未标记默认时回退第一条 enabled（兼容 0050 之前部署）。
func TestDefaultForwardUpstreams_无默认回退第一条启用(t *testing.T) {
	ups := []Upstream{
		{ID: "u1", Enabled: false},
		{ID: "u2", Enabled: true},
		{ID: "u3", Enabled: true},
	}
	got := DefaultForwardUpstreams(ups)
	if len(got) != 1 || got[0].ID != "u2" {
		t.Fatalf("expect fallback u2, got %+v", got)
	}
	if got := DefaultForwardUpstreams(nil); got != nil {
		t.Fatalf("empty should be nil, got %+v", got)
	}
}

// 显式默认全部停用时不得回落到非默认上游（M6-042：避免内网 DNS 进入 "."）。
func TestDefaultForwardUpstreams_显式默认全停用不回落(t *testing.T) {
	ups := []Upstream{
		{ID: "intranet", Addrs: []string{"10.0.0.53"}, Enabled: true},
		{ID: "default", Addrs: []string{"223.5.5.5"}, Enabled: false, IsDefault: true},
	}
	if got := DefaultForwardUpstreams(ups); len(got) != 0 {
		t.Fatalf("explicit defaults disabled must not fall back, got %+v", got)
	}
}

func TestValidateDefaultUpstream(t *testing.T) {
	cases := []struct {
		name    string
		u       Upstream
		wantErr bool
	}{
		{"非默认不校验私网", Upstream{Addrs: []string{"10.0.0.53"}, IsDefault: false}, false},
		{"默认公网 IPv4", Upstream{Addrs: []string{"114.114.114.114:53", "223.5.5.5"}, IsDefault: true}, false},
		{"默认公网 IPv6", Upstream{Addrs: []string{"[2400:3200::1]:53"}, IsDefault: true}, false},
		{"默认私网 RFC1918", Upstream{Addrs: []string{"10.59.1.142:53"}, IsDefault: true}, true},
		{"默认环回", Upstream{Addrs: []string{"127.0.0.1"}, IsDefault: true}, true},
		{"默认链路本地", Upstream{Addrs: []string{"169.254.1.1"}, IsDefault: true}, true},
		{"默认 ULA", Upstream{Addrs: []string{"[fd00::53]:53"}, IsDefault: true}, true},
		{"默认 CGNAT", Upstream{Addrs: []string{"100.64.0.1"}, IsDefault: true}, true},
		{"默认非 IP", Upstream{Addrs: []string{"dns.example.com"}, IsDefault: true}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDefaultUpstream(tc.u)
			if tc.wantErr != errors.Is(err, ErrDefaultUpstreamNotPublic) {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

// 服务层拦截：私网地址标记默认时不落库。
func TestService_默认标记私网拒绝(t *testing.T) {
	repo := NewMemUpstreamRepo()
	svc := NewService(repo, NewProber(0, nil), &fakeCtl{})
	_, err := svc.Create(t.Context(), Upstream{
		Name: "内网", Addrs: []string{"10.0.0.53"}, Protocol: "udp", IsDefault: true, Enabled: true,
	})
	if !errors.Is(err, ErrDefaultUpstreamNotPublic) {
		t.Fatalf("expect DEFAULT_UPSTREAM_NOT_PUBLIC, got %v", err)
	}
	if list, _ := repo.List(t.Context()); len(list) != 0 {
		t.Fatalf("must not persist, got %+v", list)
	}
}
