package kea

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/xiaodaoi/ipam/internal/module/dhcp"
	"github.com/xiaodaoi/ipam/internal/module/ipam"
)

func TestBuildConfigFull_注入选项与类(t *testing.T) {
	cfg, err := BuildConfigFull(
		[]ipam.Subnet{{Family: 4, CIDR: "192.168.9.0/24", KeaSubnetID: 9}},
		[]dhcp.DhcpOption{
			{OptionCode: 3, Name: "routers", Data: "192.168.9.1", Enabled: true},
			{OptionCode: 6, Name: "domain-name-servers", Data: "1.1.1.1", Enabled: false},
		},
		[]dhcp.DhcpClass{
			{Name: "printers", Test: "option[61].hex == option[61].hex", Enabled: true,
				Options: []dhcp.ClassOption{{OptionCode: 3, Name: "routers", Data: "192.168.9.254"}}},
			{Name: "offclass", Test: "x", Enabled: false},
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(cfg.Dhcp4)
	s := string(b)
	for _, want := range []string{`"client-classes"`, `printers`, `option[61].hex == option[61].hex`, `"option-data"`, `"code":3`, `192.168.9.1`, `192.168.9.254`} {
		if !strings.Contains(s, want) {
			t.Fatalf("生成配置缺少 %s: %s", want, s)
		}
	}
	for _, banned := range []string{"offclass", "1.1.1.1"} {
		if strings.Contains(s, banned) {
			t.Fatalf("disabled 条目不应进配置: %s", banned)
		}
	}
}

func TestBuildConfig_空选项保持基础形态(t *testing.T) {
	cfg, err := BuildConfigFull([]ipam.Subnet{}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Dhcp4["client-classes"]; ok {
		t.Fatal("空类不应注入 client-classes 键")
	}
	if _, ok := cfg.Dhcp4["option-data"]; ok {
		t.Fatal("空选项不应注入 option-data 键")
	}
}

func TestBuildConfigFull_hostReservations投影(t *testing.T) {
	cfg, err := BuildConfigFull(
		[]ipam.Subnet{{Family: 4, CIDR: "192.168.9.0/24", KeaSubnetID: 9}},
		nil, nil,
		[]ipam.Reservation{
			{MAC: "aa:bb:cc:dd:ee:01", IPv4: "192.168.9.40"},
			{MAC: "", IPv4: "192.168.9.41"},
			{MAC: "aa:bb:cc:dd:ee:02", IPv4: "10.99.9.40"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(cfg.Dhcp4)
	s := string(b)
	if !strings.Contains(s, `"reservations"`) || !strings.Contains(s, "aa:bb:cc:dd:ee:01") {
		t.Fatalf("bind 行应投影进子网 reservations: %s", s)
	}
	if strings.Contains(s, "192.168.9.41") || strings.Contains(s, "10.99.9.40") {
		t.Fatalf("reserve 行与跨网段地址不应投影: %s", s)
	}
}

func TestBuildConfig6_含pdPools(t *testing.T) {
	plen, dlen := 64, 80
	cfg, err := BuildConfig6([]ipam.Subnet{
		{Family: 6, CIDR: "2406:172::/64", KeaSubnetID: 1, Pools: []ipam.Pool{
			{StartAddr: "2406:172::", Kind: "pd", PrefixLen: &plen, DelegatedLen: &dlen},
			{StartAddr: "2406:172::10", EndAddr: "2406:172::20", Kind: "dynamic"},
		}},
		{Family: 4, CIDR: "10.0.0.0/24"},
		{Family: 6, CIDR: "2406:173::/64", Pools: []ipam.Pool{
			{StartAddr: "2406:173::", Kind: "pd"}, // 缺 len 剔除
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(cfg.Dhcp6)
	var parsed struct {
		Subnet6 []struct {
			Subnet  string           `json:"subnet"`
			PdPools []map[string]any `json:"pd-pools"`
			Pools   []map[string]any `json:"pools"`
		} `json:"subnet6"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Subnet6) != 2 {
		t.Fatalf("v4 过滤后 subnet6 应 2 个: %d", len(parsed.Subnet6))
	}
	first, second := parsed.Subnet6[0], parsed.Subnet6[1]
	if first.Subnet != "2406:172::/64" || len(first.PdPools) != 1 {
		t.Fatalf("pd-pools 投影异常: %+v", first)
	}
	if first.PdPools[0]["prefix-len"].(float64) != 64 || first.PdPools[0]["delegated-len"].(float64) != 80 {
		t.Fatalf("pd-pools len 异常: %+v", first.PdPools[0])
	}
	if len(first.Pools) != 1 || !strings.Contains(first.Pools[0]["pool"].(string), "2406:172::10") {
		t.Fatalf("dynamic pool 缺失: %+v", first.Pools)
	}
	// 缺 len 的 pd 池不投影（子网本身保留，无 pools/pd-pools 键）
	if second.Subnet != "2406:173::/64" || second.PdPools != nil || second.Pools != nil {
		t.Fatalf("缺 len 池的子网不应携带池: %+v", second)
	}
}

func TestBuildConfig_子网级optionData(t *testing.T) {
	// M2-019：子网级网关/DNS → subnet option-data（覆盖全局）
	cfg, err := BuildConfig([]ipam.Subnet{
		{Family: 4, CIDR: "10.99.3.0/24", KeaSubnetID: 6, Gateway: "10.99.3.1",
			DNSServers: "223.5.5.5, 114.114.114.114",
			Pools:      []ipam.Pool{{StartAddr: "10.99.3.10", EndAddr: "10.99.3.100", Kind: "dynamic"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(cfg.Dhcp4)
	s := string(b)
	for _, want := range []string{`"option-data"`, `"routers"`, `10.99.3.1`, `"domain-name-servers"`, `223.5.5.5`} {
		if !strings.Contains(s, want) {
			t.Fatalf("子网级 option-data 缺失 %s: %s", want, s)
		}
	}
}

func TestBuildConfig6_子网级dnsServers(t *testing.T) {
	cfg, err := BuildConfig6([]ipam.Subnet{
		{Family: 6, CIDR: "2406:174::/64", KeaSubnetID: 7, DNSServers: "2406:174::53",
			Pools: []ipam.Pool{{StartAddr: "2406:174::", Kind: "pd", PrefixLen: ipamInt(64), DelegatedLen: ipamInt(80)}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(cfg.Dhcp6)
	s := string(b)
	if !strings.Contains(s, `"dns-servers"`) || !strings.Contains(s, "2406:174::53") {
		t.Fatalf("v6 dns-servers 缺失: %s", s)
	}
	if !strings.Contains(s, `"pd-pools"`) {
		t.Fatalf("pd-pools 缺失: %s", s)
	}
}

func ipamInt(v int) *int { return &v }
