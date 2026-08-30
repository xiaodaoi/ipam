// Package kea 引擎编排：子网配置生成与 ctrl-agent 下发（§2.2 三步走）。
package kea

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/xiaodaoi/ipam/internal/module/ipam"
)

// CtrlAgent 客户端：config-set（Kea 原子校验+应用，解析失败自动保留旧配置=回滚语义）。
type CtrlAgent struct {
	baseURL string
	hc      *http.Client
}

func NewCtrlAgent(baseURL string) *CtrlAgent {
	return &CtrlAgent{baseURL: strings.TrimSuffix(baseURL, "/"), hc: &http.Client{Timeout: 10e9}}
}

type commandResp struct {
	Result int             `json:"result"`
	Text   string          `json:"text"`
	Args   json.RawMessage `json:"arguments,omitempty"`
}

// Command 发送单条控制命令（config-set / subnet4-del 等）。
func (c *CtrlAgent) Command(ctx context.Context, command string, service string, args any) (commandResp, error) {
	payload := map[string]any{
		"command": command,
		"service": []string{service},
	}
	if args != nil {
		payload["arguments"] = args
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/", bytes.NewReader(body))
	if err != nil {
		return commandResp{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return commandResp{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return commandResp{}, fmt.Errorf("ctrl-agent http %d: %s", resp.StatusCode, raw)
	}
	// Kea ctrl-agent 返回数组（每 service 一项），取首项
	var list []commandResp
	if err := json.Unmarshal(raw, &list); err != nil || len(list) == 0 {
		return commandResp{}, fmt.Errorf("bad resp: %v (%s)", err, raw)
	}
	out := list[0]
	if out.Result != 0 && out.Result != 3 { // 3=empty：list/get 类命令的空结果是成功（M2-031 回归修复）
		return out, fmt.Errorf("kea command %s failed: %s", command, out.Text)
	}
	return out, nil
}

// DeploySubnet 实现 ipam.KeaDeployer：dryRun 不联网（本地生成校验）；
// 真实模式经 ctrl-agent config-set 整段下发，并读回 kea 分配的第一个 subnet-id。
func (c *CtrlAgent) DeploySubnet(ctx context.Context, subnets []ipam.Subnet, dryRun bool) (int, error) {
	cfg, err := BuildConfig(subnets)
	if err != nil {
		return 0, err
	}
	if dryRun {
		if _, err := BuildConfig6(subnets); err != nil { // M2-018：v6 投影本地校验
			return 0, err
		}
		if len(subnets) > 0 {
			return subnets[0].KeaSubnetID, nil
		}
		return 1, nil
	}
	if len(subnets) == 0 {
		return 0, fmt.Errorf("empty subnet list")
	}
	resp, err := c.Command(ctx, "config-set", "dhcp4", cfg)
	if err != nil {
		return 0, err
	}
	_ = resp
	// M2-018：存在 v6 子网时同步下发 Dhcp6（经 agent 转发 dhcp6 socket）
	cfg6, err6 := BuildConfig6(subnets)
	if err6 != nil {
		return 0, err6
	}
	if s6, ok := cfg6.Dhcp6["subnet6"].([]map[string]any); ok && len(s6) > 0 {
		if _, err := c.Command(ctx, "config-set", "dhcp6", cfg6); err != nil {
			return 0, err
		}
	}
	// 首个 subnet 的 id 由服务端按序分配（1..n）；本地计算同步
	id := 1
	for _, s := range subnets {
		if s.KeaSubnetID > id {
			id = s.KeaSubnetID
		}
	}
	return id, nil
}

// BindStatic host reservation 下发（reservation-add 语义）。
func (c *CtrlAgent) BindStatic(ctx context.Context, subnetID, addr, mac string) error {
	_, err := c.Command(ctx, "reservation-add", "dhcp4", map[string]any{
		"reservation": map[string]any{
			"hw-address": mac,
			"ip-address": addr,
		},
	})
	return err
}

// RemoveSubnet 摘除指定 subnet-id（真实模式）。
func (c *CtrlAgent) RemoveSubnet(ctx context.Context, subnetID int) error {
	if subnetID <= 0 {
		return nil // dryRun/未下发
	}
	_, err := c.Command(ctx, "subnet4-del", "dhcp4", map[string]any{"id": subnetID})
	return err
}

// Lease6 单条 DHCPv6 租约投影（M2-022，lease_cmds hook 实时查询）。
type Lease6 struct {
	IPAddress     string `json:"ip-address"`
	LeaseType     string `json:"type"`
	PrefixLen     int    `json:"prefix-len,omitempty"`
	DUID          string `json:"duid"`
	IAID          uint32 `json:"iaid,omitempty"`
	CLTT          int64  `json:"cltt,omitempty"`
	ValidLifetime uint32 `json:"valid-lft,omitempty"`
	HWAddress     string `json:"hw-address,omitempty"` // relay option79（RFC 6939）或 DUID-LL 解析；无则空
	HWType        int    `json:"hwtype,omitempty"`
	HWSource      int    `json:"hwaddr-source,omitempty"`
}

// Lease6List 实时查询 Kea DHCPv6 租约（PD/NA；memfile 不入库）。
// result 3 表示空结果（0 租约），视为空列表。
func (c *CtrlAgent) Lease6List(ctx context.Context) ([]Lease6, error) {
	resp, err := c.Command(ctx, "lease6-get-all", "dhcp6", nil)
	if err != nil {
		return nil, err
	}
	if resp.Result != 0 && resp.Result != 3 {
		return nil, fmt.Errorf("lease6-get-all: result=%d %s", resp.Result, resp.Text)
	}
	var out struct {
		Leases []Lease6 `json:"leases"`
	}
	if len(resp.Args) > 0 {
		if err := json.Unmarshal(resp.Args, &out); err != nil {
			return nil, err
		}
	}
	return out.Leases, nil
}
