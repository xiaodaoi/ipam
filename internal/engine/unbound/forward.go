// Package unbound 引擎编排：forward-zone 下发与配置合成（§2.3）。
package unbound

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"time"

	"github.com/xiaodaoi/ipam/internal/module/dns"
)

// ErrUnavailable unbound-control 不可用（与 coherence 侧语义一致）。
var ErrUnavailable = fmt.Errorf("unbound-control unavailable")

// ExecController unbound-control 下发（forward_add/forward_remove）。
type ExecController struct {
	Bin string // 缺省 unbound-control；测试注入 fake
}

func (e ExecController) run(args ...string) error {
	bin := e.Bin
	if bin == "" {
		bin = "unbound-control"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return ErrUnavailable
	}
	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %v: %s", bin, args, err, out)
	}
	return nil
}

// SyncForward 以全量 enabled 上游收敛 forward-zone "."。
// 简化语义：先移除旧默认区（ignore 错误=首次），再按上游 forward_add。
func (e ExecController) SyncForward(_ context.Context, upstreams []dns.Upstream) error {
	if len(upstreams) == 0 {
		return nil
	}
	enabled := make([]dns.Upstream, 0, len(upstreams))
	for _, u := range upstreams {
		if u.Enabled {
			enabled = append(enabled, u)
		}
	}
	if len(enabled) == 0 {
		return nil
	}
	args := []string{"forward_add", "."}
	for _, u := range enabled {
		for _, a := range u.Addrs {
			host, port, err := net.SplitHostPort(normalizeAddr(a))
			if err != nil {
				return err
			}
			args = append(args, fmt.Sprintf("%s@%s", host, port))
		}
	}
	return e.run(args...)
}

// SyncForwardRules 条件转发规则下发（每条 forward_add <domain> <addrs...>）。
func (e ExecController) SyncForwardRules(_ context.Context, rules []dns.ForwardRule, ups []dns.Upstream) error {
	byID := map[string]dns.Upstream{}
	for _, u := range ups {
		byID[u.ID] = u
	}
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		args := []string{"forward_add", r.Domain}
		for _, id := range r.UpstreamIDs {
			u, ok := byID[id]
			if !ok || !u.Enabled {
				continue
			}
			for _, a := range u.Addrs {
				host, port, err := net.SplitHostPort(normalizeAddr(a))
				if err != nil {
					return err
				}
				args = append(args, fmt.Sprintf("%s@%s", host, port))
			}
		}
		if len(args) > 2 {
			if err := e.run(args...); err != nil {
				return err
			}
		}
	}
	return nil
}

// normalizeAddr 补默认端口（"223.5.5.5" → "223.5.5.5:53"）。
func normalizeAddr(a string) string {
	if _, _, err := net.SplitHostPort(a); err == nil {
		return a
	}
	return net.JoinHostPort(a, "53")
}

// DialProbe TCP:53 连通性探测（F-R4 探活基础实现）。
func DialProbe(ctx context.Context, addr string) (time.Duration, error) {
	start := time.Now()
	d := net.Dialer{Timeout: 2 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", normalizeAddr(addr))
	if err != nil {
		return 0, err
	}
	_ = conn.Close()
	return time.Since(start), nil
}
