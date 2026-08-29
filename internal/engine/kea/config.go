package kea

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"strings"

	"github.com/xiaodaoi/ipam/internal/module/dhcp"
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
		sub := map[string]any{
			"id":     s.KeaSubnetID,
			"subnet": s.CIDR,
			"pools":  pools,
		}
		// 子网级 option-data（M2-019：网关/DNS，覆盖全局）
		var od []map[string]any
		if s.Gateway != "" {
			od = append(od, map[string]any{"name": "routers", "data": s.Gateway})
		}
		if s.DNSServers != "" {
			od = append(od, map[string]any{"name": "domain-name-servers", "data": s.DNSServers})
		}
		if len(od) > 0 {
			sub["option-data"] = od
		}
		sub4 = append(sub4, sub)
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

// BuildConfigFull 在 BuildConfig 之上注入全局 option-data（C-02）与 client-classes（C-03）。
// disabled 条目不进配置；空切片不注入对应键（保持基础模板形态）。下发沿 config-set 原子语义。
func BuildConfigFull(subnets []ipam.Subnet, opts []dhcp.DhcpOption, classes []dhcp.DhcpClass, binds []ipam.Reservation) (Dhcp4Config, error) {
	cfg, err := BuildConfig(subnets)
	if err != nil {
		return cfg, err
	}
	var globalOD []map[string]any
	for _, o := range opts {
		if !o.Enabled {
			continue
		}
		globalOD = append(globalOD, map[string]any{"code": o.OptionCode, "data": o.Data})
	}
	if len(globalOD) > 0 {
		cfg.Dhcp4["option-data"] = globalOD
	}
	var ccs []map[string]any
	for _, c := range classes {
		if !c.Enabled {
			continue
		}
		od := make([]map[string]any, 0, len(c.Options))
		for _, co := range c.Options {
			od = append(od, map[string]any{"code": co.OptionCode, "data": co.Data})
		}
		ccs = append(ccs, map[string]any{"name": c.Name, "test": c.Test, "option-data": od})
	}
	if len(ccs) > 0 {
		cfg.Dhcp4["client-classes"] = ccs
	}
	// host reservations（M3-007 配置式 bind）：按地址归属子网投影到 reservations 段。
	// 仅 MAC 非空行（reserve 冻结语义不走 host reservation）；跨网段地址不投影。
	if len(binds) > 0 {
		if sub4, ok := cfg.Dhcp4["subnet4"].([]map[string]any); ok {
			for i := range sub4 {
				cidr, _ := sub4[i]["subnet"].(string)
				p, perr := netip.ParsePrefix(strings.TrimSpace(cidr))
				if perr != nil {
					continue
				}
				var resv []map[string]any
				for _, b := range binds {
					if b.MAC == "" {
						continue
					}
					if ip, ierr := netip.ParseAddr(b.IPv4); ierr == nil && p.Contains(ip) {
						resv = append(resv, map[string]any{"ip-address": b.IPv4, "hw-address": b.MAC})
					}
				}
				if len(resv) > 0 {
					sub4[i]["reservations"] = resv
				}
			}
		}
	}
	return cfg, nil
}

// Dhcp6Config v6 配置容器（M2-018：kea-dhcp6 的 config-set 载荷）。
type Dhcp6Config struct {
	Dhcp6 map[string]any `json:"Dhcp6"`
}

// BuildConfig6 由 v6 子网组装 Dhcp6 配置：dynamic 池 → pools；pd 池 → pd-pools（M2-018）。
func BuildConfig6(subnets []ipam.Subnet) (Dhcp6Config, error) {
	base := map[string]any{
		"interfaces-config": map[string]any{"interfaces": []string{"eth0"}},
		"valid-lifetime":    3600,
		"lease-database":    map[string]any{"type": "memfile"},
	}
	sub6 := make([]map[string]any, 0, len(subnets))
	for i := range subnets {
		s := subnets[i]
		if s.Family != 6 {
			continue
		}
		sub := map[string]any{"id": s.KeaSubnetID, "subnet": s.CIDR}
		if s.DNSServers != "" {
			sub["option-data"] = []map[string]any{{"name": "dns-servers", "data": s.DNSServers}}
		}
		var pools6, pdPools []map[string]any
		for _, p := range s.Pools {
			if p.Kind == "pd" {
				if p.PrefixLen != nil && p.DelegatedLen != nil {
					pdPools = append(pdPools, map[string]any{
						"prefix":        p.StartAddr,
						"prefix-len":    *p.PrefixLen,
						"delegated-len": *p.DelegatedLen,
					})
				}
				continue
			}
			pools6 = append(pools6, map[string]any{"pool": fmt.Sprintf("%s - %s", p.StartAddr, p.EndAddr)})
		}
		if len(pools6) > 0 {
			sub["pools"] = pools6
		}
		if len(pdPools) > 0 {
			sub["pd-pools"] = pdPools
		}
		sub6 = append(sub6, sub)
	}
	base["subnet6"] = sub6
	return Dhcp6Config{Dhcp6: base}, nil
}
