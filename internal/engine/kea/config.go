package kea

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/xiaodaoi/ipam/internal/module/ipam"
)

// Dhcp4Config 最小 Dhcp4 配置容器（config-set 载荷）。
type Dhcp4Config struct {
	Dhcp4 map[string]any `json:"Dhcp4"`
}

// BuildConfig 由 PG 子网集合 + 基础模板组装整段 Dhcp4 配置。
// 模板路径缺省读 config/kea/kea-dhcp4.conf；空列表时仅保留基础结构。
func BuildConfig(subnets []ipam.Subnet) (Dhcp4Config, error) {
	base := defaultBaseConfig()
	if tpl := os.Getenv("IPAM_KEA_TPL"); tpl != "" {
		data, err := os.ReadFile(tpl)
		if err != nil {
			return Dhcp4Config{}, err
		}
		if err := json.Unmarshal(data, &base); err != nil {
			return Dhcp4Config{}, err
		}
	}
	sub4 := make([]map[string]any, 0, len(subnets))
	for i, s := range subnets {
		if s.Family != 4 {
			continue
		}
		pools := []map[string]any{}
		for _, p := range s.Pools {
			pools = append(pools, map[string]any{
				"pool": fmt.Sprintf("%s - %s", p.StartAddr, p.EndAddr),
			})
		}
		sub4 = append(sub4, map[string]any{
			"id":     s.KeaSubnetID,
			"subnet": s.CIDR,
			"pools":  pools,
		})
		_ = i
	}
	base["subnet4"] = sub4
	return Dhcp4Config{Dhcp4: base}, nil
}

func defaultBaseConfig() map[string]any {
	return map[string]any{
		"interfaces-config": map[string]any{"interfaces": []string{"eth0"}},
		"control-socket": map[string]any{
			"socket-type": "unix",
			"socket-name": "/run/ipam/kea4-ctrl.sock",
		},
		"lease-database": map[string]any{"type": "memfile"},
		"valid-lifetime": 3600,
		"loggers": []any{
			map[string]any{
				"name":     "kea-dhcp4",
				"severity": "INFO",
				"output_options": []any{
					map[string]any{"output": "stdout"},
				},
			},
		},
	}
}
