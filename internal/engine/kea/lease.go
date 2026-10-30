package kea

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xiaodaoi/ipam/internal/engine/lease"
)

type lease4Resp struct {
	Leases []lease.Lease4 `json:"leases"`
}

// Lease4List 实时查询 Kea DHCPv4 租约（lease4-get-all）。
func (c *CtrlAgent) Lease4List(ctx context.Context) ([]lease.Lease4, error) {
	resp, err := c.Command(ctx, "lease4-get-all", "dhcp4", nil)
	if err != nil {
		return nil, err
	}
	var out lease4Resp
	if len(resp.Args) > 0 {
		if err := json.Unmarshal(resp.Args, &out); err != nil {
			return nil, err
		}
	}
	return out.Leases, nil
}

// Lease4Get 查询单条租约；不存在返回 ok=false。
func (c *CtrlAgent) Lease4Get(ctx context.Context, ip string) (lease.Lease4, bool, error) {
	resp, err := c.Command(ctx, "lease4-get", "dhcp4", map[string]any{"ip-address": ip})
	if err != nil {
		return lease.Lease4{}, false, err
	}
	if resp.Result == 3 {
		return lease.Lease4{}, false, nil
	}
	var out lease.Lease4
	if len(resp.Args) > 0 {
		if err := json.Unmarshal(resp.Args, &out); err != nil {
			return lease.Lease4{}, false, err
		}
	}
	return out, true, nil
}

// Lease4Update 延期/改绑/改主机名（lease4-update，按现有 subnet-id 定位）。
func (c *CtrlAgent) Lease4Update(ctx context.Context, l lease.Lease4) error {
	if l.SubnetID == 0 {
		return fmt.Errorf("lease4-update: subnet-id 缺失")
	}
	args := map[string]any{
		"subnet-id":  l.SubnetID,
		"ip-address": l.IPAddress,
		"valid-lft":  l.ValidLifetime,
		"cltt":       l.Cltt,
		"state":      l.State,
	}
	if l.HWAddress != "" {
		args["hw-address"] = l.HWAddress
	}
	if l.ClientID != "" {
		args["client-id"] = l.ClientID
	}
	args["hostname"] = l.Hostname
	_, err := c.Command(ctx, "lease4-update", "dhcp4", args)
	return err
}

// Lease4Delete 删除租约（不存在视为成功，幂等）。
func (c *CtrlAgent) Lease4Delete(ctx context.Context, ip string) error {
	_, err := c.Command(ctx, "lease4-del", "dhcp4", map[string]any{"ip-address": ip})
	return err
}

// Lease6Delete 删除 DHCPv6 租约（IA_NA/IA_PD；不存在视为成功）。
func (c *CtrlAgent) Lease6Delete(ctx context.Context, ip, leaseType string) error {
	leaseType = strings.ToUpper(strings.TrimSpace(leaseType))
	if leaseType == "" {
		leaseType = "IA_NA"
	}
	_, err := c.Command(ctx, "lease6-del", "dhcp6", map[string]any{
		"ip-address": ip,
		"type":       leaseType,
	})
	return err
}
