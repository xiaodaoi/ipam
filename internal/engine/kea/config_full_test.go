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
			{Name: "printers", Test: "option[61].hex == option[61].hex '.*'", Enabled: true,
				Options: []dhcp.ClassOption{{OptionCode: 3, Name: "routers", Data: "192.168.9.254"}}},
			{Name: "offclass", Test: "x", Enabled: false},
		},
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
	cfg, err := BuildConfigFull([]ipam.Subnet{}, nil, nil)
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
