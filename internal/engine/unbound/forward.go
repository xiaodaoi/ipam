// Package unbound 引擎编排：forward-zone 下发与配置合成（§2.3）。
package unbound

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/xiaodaoi/ipam/internal/module/dns"
)

// ErrUnavailable unbound-control 不可用（与 coherence 侧语义一致）。
var ErrUnavailable = fmt.Errorf("unbound-control unavailable")

// ExecController unbound-control 下发（forward_add/forward_remove）。
type ExecController struct {
	Bin  string // 缺省 unbound-control；测试注入 fake
	Conf string // 客户端配置路径（-c 注入）；空=容器默认 conf（M3-008：sock 通道注入）
}

// cmdArgs 组装命令行（-c 前置注入客户端配置）。
func (e ExecController) cmdArgs(args ...string) []string {
	if e.Conf != "" {
		return append([]string{"-c", e.Conf}, args...)
	}
	return args
}

func (e ExecController) run(args ...string) error {
	bin := e.Bin
	if bin == "" {
		bin = "unbound-control"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return ErrUnavailable
	}
	out, err := exec.Command(bin, e.cmdArgs(args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %v: %s", bin, args, err, out)
	}
	return nil
}

// SyncForward 收敛 forward-zone "." 为默认公网出口池（M6-041）：
// 所有 enabled 且 is_default 的上游地址全部下发，实现多地址冗余；
// 未标记任何默认时回退第一条 enabled（兼容旧数据）。
// 默认 "." 只承载公网回落，不得混入内网 DNS——否则禁用某内网域的规则后，
// 该域仍经 "." 回落到其它内网 DNS 继续解析，禁用失效（M3-011 现场）。
func (e ExecController) SyncForward(_ context.Context, upstreams []dns.Upstream) error {
	defs := dns.DefaultForwardUpstreams(upstreams)
	if len(defs) == 0 {
		return nil
	}
	args := []string{"forward_add", "."}
	seen := map[string]bool{}
	for _, u := range defs {
		for _, a := range u.Addrs {
			host, port, err := net.SplitHostPort(NormalizeAddr(a))
			if err != nil {
				return err
			}
			target := fmt.Sprintf("%s@%s", host, port)
			if seen[target] {
				continue
			}
			seen[target] = true
			args = append(args, target)
		}
	}
	if len(args) == 2 {
		return nil
	}
	return e.run(args...)
}

// CheckConf 校验候选配置：候选块自包含（interface 等静态身份段归主 conf，M3-009），
// 独立写临时文件执行 unbound-checkconf 校验语法。
// 不得拼接 confPath 旧内容——旧区与候选区重复会触发
// "duplicate forward zone" 误报使 apply 整体拒绝（M3-011 事故：渲染文件从未更新成功）。
func (e ExecController) CheckConf(_ context.Context, confPath, renderedBlock string) error {
	check := "unbound-checkconf"
	if _, err := exec.LookPath(check); err != nil {
		return ErrUnavailable
	}
	tmp, err := os.CreateTemp("", "ipam-checkconf-*.conf")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(candidateConfText(renderedBlock)); err != nil {
		_ = tmp.Close()
		return err
	}
	_ = tmp.Close()
	out, err := exec.Command(check, tmp.Name()).CombinedOutput()
	if err != nil {
		_ = os.WriteFile("/tmp/ipam-checkconf-fail.conf", []byte(renderedBlock), 0o644)
		log.Printf("[checkconf] 候选配置校验失败，已写 /tmp/ipam-checkconf-fail.conf：%s", out)
		return fmt.Errorf("checkconf: %v: %s", err, out)
	}
	return nil
}

// candidateConfText 组装校验用文本。
//
// M6-038：RPZ 区要求 module-config 含 respip，否则 checkconf 报
// "RPZ requires the respip module" 使整次 apply 被拒。主 conf 的
// "python respip iterator" 不宜在此复用——校验容器不带 python 运行时，
// 且 python 模块与候选块无关；只补 respip 即可覆盖 RPZ 语义校验。
func candidateConfText(renderedBlock string) string {
	head := "server:\n# candidate\n"
	if strings.Contains(renderedBlock, "rpz:") {
		head = "server:\n  module-config: \"respip iterator\"\n# candidate\n"
	}
	return head + renderedBlock
}

// Reload 全量 reload。
func (e ExecController) Reload(ctx context.Context) error {
	return e.run("reload")
}

// FlushZone 清空缓存（zone 空=flush 全部）。
func (e ExecController) FlushZone(_ context.Context, zone string) error {
	if zone == "" {
		return e.run("flush")
	}
	return e.run("flush_zone", zone)
}

// AuthZoneReload 单区刷新（auth_zone_reload <zone>，不动整进程，§2.3）。
func (e ExecController) AuthZoneReload(_ context.Context, zoneID string) error {
	if zoneID == "" {
		return ErrUnavailable
	}
	return e.run("auth_zone_reload", zoneID)
}

// SyncForwardRules 条件转发规则下发（每条 forward_add <domain> <addrs...>）。
func (e ExecController) SyncForwardRules(_ context.Context, rules []dns.ForwardRule, ups []dns.Upstream) error {
	byID := map[string]dns.Upstream{}
	for _, u := range ups {
		byID[u.ID] = u
	}
	for _, r := range rules {
		if !r.Enabled || r.Domain == "." {
			// "." 由默认公网出口池统一管理（M6-041），规则不再覆盖默认转发。
			continue
		}
		args := []string{"forward_add", r.Domain}
		for _, id := range r.UpstreamIDs {
			u, ok := byID[id]
			if !ok || !u.Enabled {
				continue
			}
			for _, a := range u.Addrs {
				host, port, err := net.SplitHostPort(NormalizeAddr(a))
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

// NormalizeAddr 补默认端口（导出供 main 装配使用）。
func NormalizeAddr(a string) string {
	if _, _, err := net.SplitHostPort(a); err == nil {
		return a
	}
	return net.JoinHostPort(a, "53")
}

// DialProbe TCP:53 连通性探测（F-R4 探活基础实现）。
func DialProbe(ctx context.Context, addr string) (time.Duration, error) {
	start := time.Now()
	d := net.Dialer{Timeout: 2 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", NormalizeAddr(addr))
	if err != nil {
		return 0, err
	}
	_ = conn.Close()
	return time.Since(start), nil
}

// LocalZone 运行时本地域（M2-033 黑名单：local_zone static <name> → NXDOMAIN 拦截；
// RPZ auth-zone 路线在容器 unbound 1.26 编译下不可用——auth-zone 内 rpz: yes 实测 unknown keyword）。
func (e ExecController) LocalZone(_ context.Context, name, zoneType string) error {
	return e.run("local_zone", name, zoneType)
}

// LocalZoneRemove 移除本地域。
func (e ExecController) LocalZoneRemove(_ context.Context, name string) error {
	return e.run("local_zone_remove", name)
}

// LocalData 运行时本地记录（unbound-control local_data "<name> <ttl> IN <type> <rdata>"）。
func (e ExecController) LocalData(_ context.Context, data string) error {
	return e.run("local_data", data)
}

// LocalDataRemove 移除本地记录。
func (e ExecController) LocalDataRemove(_ context.Context, name string) error {
	return e.run("local_data_remove", name)
}
