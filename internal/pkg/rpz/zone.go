// Package rpz 提供 RPZ 策略区渲染与 unbound 运行态解析（M6-038）。
//
// 只做纯文本/纯数据变换，不依赖 dns 业务模块与 unbound 引擎，便于两侧复用与单测。
package rpz

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// RPZ 动作（与 blocklist_entry.action 及白名单语义对齐）。
const (
	ActionNXDomain = "nxdomain"
	ActionDrop     = "drop"
	ActionTCPOnly  = "tcp_only"
	ActionRedirect = "redirect"
	ActionPassthru = "passthru"
)

// defaultTTL 本地数据记录默认 TTL（提示页跳转）。
const defaultTTL = 300

// Record 一条策略记录（一个域名可带 "*.name" 通配前缀）。
type Record struct {
	Name   string // 域名，如 example.com 或 *.example.com
	Action string // nxdomain|drop|tcp_only|redirect|passthru
	IP     string // Action=redirect 时的提示页 IP
}

// AuthZone unbound list_auth_zones 的一行（RPZ 区也在此列）。
type AuthZone struct {
	Name   string
	Serial uint32
	State  string // serial 缺失时的原始状态（如 expired）
}

// actionPriority 同名冲突时的优先级（数值大者胜）。
// 白名单在独立 zone 中，这里只用于自定义条目的确定性收敛：
// 提示页比直接 NXDOMAIN 对用户更友好，drop/tcp_only 次之。
var actionPriority = map[string]int{
	ActionNXDomain: 1,
	ActionTCPOnly:  2,
	ActionDrop:     3,
	ActionRedirect: 4,
	ActionPassthru: 5,
}

type owner struct {
	action string
	ip     string
	wild   bool // 是否同时输出 *.name
}

// Render 渲染 RPZ 区文本。origin 为区名（可带尾点），serial 写入 SOA。
// 返回渲染文本与去重后的域名条目数（owner 数）。
//
// 记录以相对区名（不带尾点）输出，按字典序排列以保证同样输入产生同样字节，
// 便于用 sha256 判断是否需要重新下发。
func Render(origin string, serial uint32, records []Record) (string, int) {
	origin = strings.TrimSuffix(strings.TrimSpace(origin), ".") + "."

	owners := map[string]*owner{}
	for _, r := range records {
		name := strings.ToLower(strings.TrimSpace(r.Name))
		isWild := strings.HasPrefix(name, "*.")
		if isWild {
			name = strings.TrimPrefix(name, "*.")
		}
		name = strings.TrimSuffix(name, ".")
		if !validName(name) {
			continue
		}
		act := strings.ToLower(strings.TrimSpace(r.Action))
		if _, ok := actionPriority[act]; !ok {
			act = ActionNXDomain
		}
		ip := strings.TrimSpace(r.IP)
		if act == ActionRedirect && ip == "" {
			// 未配置提示页 IP 时退化为 NXDOMAIN（与旧 local_zone 语义一致）。
			act = ActionNXDomain
		}
		cur, ok := owners[name]
		if !ok {
			owners[name] = &owner{action: act, ip: ip, wild: isWild}
			continue
		}
		if actionPriority[act] > actionPriority[cur.action] {
			cur.action, cur.ip = act, ip
		}
		cur.wild = cur.wild || isWild
	}

	names := make([]string, 0, len(owners))
	for name := range owners {
		names = append(names, name)
	}
	sort.Strings(names)

	var sb strings.Builder
	fmt.Fprintf(&sb, "$TTL %d\n", defaultTTL)
	fmt.Fprintf(&sb, "$ORIGIN %s\n", origin)
	fmt.Fprintf(&sb, "@ SOA localhost. hostmaster.localhost. %d 3600 600 86400 %d\n", serial, defaultTTL)
	sb.WriteString("@ NS localhost.\n")

	for _, name := range names {
		o := owners[name]
		// 关键：RPZ 区内的 owner 必须写成「相对区名」的形式
		// （www.example.com → www.example.com.<rpz-zone>.），unbound 匹配时
		// 去掉区名后缀得到真实查询名。写成绝对名（尾点）会被当成区外数据忽略，
		// 表现为策略不生效（M6-038 现场实测）。
		writeRecord(&sb, name, o.action, o.ip)
		if o.wild {
			writeRecord(&sb, "*."+name, o.action, o.ip)
		}
	}
	return sb.String(), len(owners)
}

func writeRecord(sb *strings.Builder, fqdn, action, ip string) {
	switch action {
	case ActionDrop:
		fmt.Fprintf(sb, "%s CNAME rpz-drop.\n", fqdn)
	case ActionTCPOnly:
		fmt.Fprintf(sb, "%s CNAME rpz-tcp-only.\n", fqdn)
	case ActionPassthru:
		fmt.Fprintf(sb, "%s CNAME rpz-passthru.\n", fqdn)
	case ActionRedirect:
		// RPZ Local Data：直接给出应答地址（提示页）。
		fmt.Fprintf(sb, "%s A %s\n", fqdn, ip)
	default: // ActionNXDomain
		fmt.Fprintf(sb, "%s CNAME .\n", fqdn)
	}
}

// validName 域名合法性：非空、含至少一个点、仅字母数字与 - _ 和点、无连续空标签。
func validName(name string) bool {
	if name == "" || len(name) > 253 || !strings.Contains(name, ".") {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for _, c := range label {
			switch {
			case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
			default:
				return false
			}
		}
	}
	return true
}

// Owners 提取 zone 文本中的 owner 名（不含通配与指令行）。
// 用于编译前后对比，决定哪些名字需要 flush 缓存（M6-038 立即生效）。
func Owners(text string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "$") || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "@") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.ToLower(strings.TrimSuffix(fields[0], "."))
		if name == "" || strings.Contains(name, "*") {
			continue
		}
		out[name] = struct{}{}
	}
	return out
}

// OwnerDiff 返回「新增」与「移除」的 owner（用于按需 flush）。
func OwnerDiff(before, after map[string]struct{}) (added, removed []string) {
	for n := range after {
		if _, ok := before[n]; !ok {
			added = append(added, n)
		}
	}
	for n := range before {
		if _, ok := after[n]; !ok {
			removed = append(removed, n)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

// ParseAuthZones 解析 unbound-control list_auth_zones 输出。
// 样例：
//
//	wl.whitelist.rpz.	serial 5	 since 1794998065 2026-11-18T10:34:25
//	intel.hot.rpz.	expired
func ParseAuthZones(out string) []AuthZone {
	zones := []AuthZone{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		z := AuthZone{Name: strings.TrimSuffix(fields[0], ".")}
		gotSerial := false
		for i := 0; i+1 < len(fields); i++ {
			if fields[i] == "serial" {
				if n, err := strconv.ParseUint(fields[i+1], 10, 32); err == nil {
					z.Serial = uint32(n)
					gotSerial = true
				}
				break
			}
		}
		if !gotSerial {
			z.State = fields[1]
		}
		zones = append(zones, z)
	}
	return zones
}

// SerialMap 把解析结果转成 zone→serial（仅包含正常加载的区）。
func SerialMap(zones []AuthZone) map[string]uint32 {
	out := map[string]uint32{}
	for _, z := range zones {
		if z.Serial > 0 {
			out[z.Name] = z.Serial
		}
	}
	return out
}
