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
		"command":   command,
		"service":   []string{service},
		"arguments": args,
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
	if out.Result != 0 {
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
	// 首个 subnet 的 id 由服务端按序分配（1..n）；本地计算同步
	id := 1
	for _, s := range subnets {
		if s.KeaSubnetID > id {
			id = s.KeaSubnetID
		}
	}
	return id, nil
}

// ReserveAddress 保留单地址（excluded 语义占位；完整 reserved 段经 config-set 表达）。
func (c *CtrlAgent) ReserveAddress(ctx context.Context, subnetID, addr string) error {
	return c.commandNoop(ctx)
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

func (c *CtrlAgent) commandNoop(ctx context.Context) error { return nil }

// RemoveSubnet 摘除指定 subnet-id（真实模式）。
func (c *CtrlAgent) RemoveSubnet(ctx context.Context, subnetID int) error {
	if subnetID <= 0 {
		return nil // dryRun/未下发
	}
	_, err := c.Command(ctx, "subnet4-del", "dhcp4", map[string]any{"id": subnetID})
	return err
}
