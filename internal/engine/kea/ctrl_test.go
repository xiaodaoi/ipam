package kea

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xiaodaoi/ipam/internal/module/ipam"
)

func TestBuildConfig_基础结构subnet4段(t *testing.T) {
	cfg, err := BuildConfig([]ipam.Subnet{
		{ID: "1", KeaSubnetID: 1, Family: 4, CIDR: "10.1.0.0/24",
			Pools: []ipam.Pool{{StartAddr: "10.1.0.10", EndAddr: "10.1.0.100", Kind: "dynamic"}}},
		{ID: "2", KeaSubnetID: 2, Family: 6, CIDR: "2406::/64"}, // v6 应被过滤
	})
	if err != nil {
		t.Fatal(err)
	}
	sub4, ok := cfg.Dhcp4["subnet4"].([]map[string]any)
	if !ok || len(sub4) != 1 {
		t.Fatalf("subnet4 = %v", cfg.Dhcp4["subnet4"])
	}
	if sub4[0]["subnet"] != "10.1.0.0/24" {
		t.Fatalf("subnet = %v", sub4[0]["subnet"])
	}
	if _, ok := cfg.Dhcp4["control-socket"]; !ok {
		t.Fatal("base template missing")
	}
}

func TestCtrlAgent_configSet成功(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" || r.Method != http.MethodPost {
			t.Errorf("unexpected req: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"result":0,"text":"Config applied"}]`))
	}))
	defer srv.Close()

	c := NewCtrlAgent(srv.URL)
	_, err := c.DeploySubnet(context.Background(),
		[]ipam.Subnet{{KeaSubnetID: 3, Family: 4, CIDR: "10.9.0.0/24"}}, false)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCtrlAgent_结果非零报错(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"result":1,"text":"invalid config"}]`))
	}))
	defer srv.Close()

	c := NewCtrlAgent(srv.URL)
	if _, err := c.DeploySubnet(context.Background(),
		[]ipam.Subnet{{KeaSubnetID: 1, Family: 4, CIDR: "10.0.0.0/24"}}, false); err == nil {
		t.Fatal("want error on result!=0")
	}
}

func TestCtrlAgent_HTTP错误(t *testing.T) {
	c := NewCtrlAgent("http://127.0.0.1:1") // 不可达端口
	if _, err := c.DeploySubnet(context.Background(),
		[]ipam.Subnet{{KeaSubnetID: 1, Family: 4, CIDR: "10.0.0.0/24"}}, false); err == nil {
		t.Fatal("want error on unreachable")
	}
}

func TestRemoveSubnet_未下发跳过(t *testing.T) {
	c := NewCtrlAgent("http://127.0.0.1:1")
	if err := c.RemoveSubnet(context.Background(), 0); err != nil {
		t.Fatalf("id<=0 should no-op: %v", err)
	}
}
