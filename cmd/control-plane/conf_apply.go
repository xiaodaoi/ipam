package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	unboundengine "github.com/xiaodaoi/ipam/internal/engine/unbound"
	dnsmodule "github.com/xiaodaoi/ipam/internal/module/dns"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// confApplier 五源合成 unbound.conf 并原子应用（§2.3 三步走收口）。
type confApplier struct {
	pool     *pgxpool.Pool
	upRepo   dnsmodule.UpstreamRepo
	frRepo   dnsmodule.ForwardRuleRepo
	zoneRepo dnsmodule.ZoneRepo
	blRepo   dnsmodule.BlocklistRepo
	settings dnsmodule.SettingsRepo
	ctl      dnsmodule.UnboundController
	confPath string
}

// apply 渲染→checkconf→原子落盘→reload；任一步失败返回错误且运行态不变。
func (a *confApplier) apply(ctx context.Context) (unboundengine.ConfInput, error) {
	in := unboundengine.ConfInput{}

	// 参数段：复用 dns 域渲染函数（§12.2 单一来源）
	if raw, ok, _ := a.settings.Get(ctx, "cache"); ok && len(raw) > 0 {
		var s struct {
			CacheMinTtl    int  `json:"cacheMinTtl"`
			CacheMaxTtl    int  `json:"cacheMaxTtl"`
			ServeExpired   bool `json:"serveExpired"`
			RrlEnabled     bool `json:"rrlEnabled"`
			RrlRate        int  `json:"rrlRate"`
			DnssecValidate bool `json:"dnssecValidate"`
		}
		_ = jsonUnmarshal(raw, &s)
		in.SettingsBlock = dnsmodule.RenderSettingsBlock(dnsmodule.Settings{
			CacheMinTtl: s.CacheMinTtl, CacheMaxTtl: s.CacheMaxTtl,
			ServeExpired: s.ServeExpired, RrlEnabled: s.RrlEnabled,
			RrlRate: s.RrlRate, DnssecValidate: s.DnssecValidate,
		})
	}

	ups, err := a.upRepo.List(ctx)
	if err != nil {
		return in, err
	}
	for _, u := range ups {
		if !u.Enabled {
			continue
		}
		for _, addr := range u.Addrs {
			in.DefaultFwd = append(in.DefaultFwd, forwardAddr(u, addr))
		}
		break // 默认转发取第一个 enabled 上游（公网回落，不含内网 DNS，M3-011）
	}

	rules, err := a.frRepo.List(ctx)
	if err != nil {
		return in, err
	}
	upByID := map[string]dnsmodule.Upstream{}
	for _, u := range ups {
		upByID[u.ID] = u
	}
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		rc := unboundengine.ForwardRuleConf{Domain: r.Domain}
		for _, id := range r.UpstreamIDs {
			u, ok := upByID[id]
			if !ok || !u.Enabled {
				continue
			}
			for _, addr := range u.Addrs {
				rc.Addrs = append(rc.Addrs, forwardAddr(u, addr))
			}
		}
		if len(rc.Addrs) > 0 {
			in.Rules = append(in.Rules, rc)
		}
	}

	zones, err := a.zoneRepo.ListZones(ctx)
	if err != nil {
		return in, err
	}
	for _, z := range zones {
		if !z.Enabled {
			continue
		}
		// secondary 区（M6-023）：axfr 从主服务器同步，仅渲染 auth-zone primary。
		if z.Kind == "secondary" {
			in.AuthZones = append(in.AuthZones, unboundengine.AuthZoneConf{
				Name:      normalizeZoneName(z.Name),
				Secondary: splitCSV(z.Primary),
			})
			continue
		}
		if z.Kind != "auth" {
			continue
		}
		records, err := a.zoneRepo.ListRecords(ctx, z.ID)
		if err != nil {
			continue
		}
		az := unboundengine.AuthZoneConf{Name: z.Name}
		wildcard := false
		for _, rec := range records {
			if !rec.Enabled {
				continue
			}
			if strings.Contains(rec.Name, "*") {
				wildcard = true
			}
			az.Records = append(az.Records, unboundengine.ZoneRecord{
				Name: fqdn(rec.Name, z.Name), Type: rec.RecType, TTL: rec.TTL, Data: rec.Rdata,
			})
		}
		// M6-023：local-data 不支持通配名，含 * 的区改用 auth-zone + zonefile。
		if wildcard {
			path, err := a.writeZonefile(z.Name, az.Records)
			if err != nil {
				return in, err
			}
			az.ZonefilePath = path
		}
		in.AuthZones = append(in.AuthZones, az)
	}

	groups, err := a.blRepo.ListPolicyGroups(ctx)
	if err != nil {
		return in, err
	}
	for _, g := range groups {
		zf := "/var/lib/ipam/rpz/" + g.ViewName + ".zone"
		// zonefile 未编译（组无条目或尚未 Replay）时跳过——缺文件会使 checkconf 整体失败（M3-011）
		if _, err := os.Stat(zf); err != nil {
			log.Printf("[conf-apply] rpz zonefile %s 不存在，跳过该 RPZ 组", zf)
			continue
		}
		in.RpzZones = append(in.RpzZones, unboundengine.RpzZoneConf{
			Name:         g.ViewName + ".rpz",
			ZonefilePath: zf,
		})
	}

	confText := unboundengine.BuildConf(in)

	// checkconf：候选全文校验
	if err := a.ctl.CheckConf(ctx, a.confPath, confText); err != nil {
		return in, err
	}

	// 原子落盘（confPath 所在目录须可写）
	tmp := a.confPath + ".candidate"
	if err := osWrite(tmp, confText); err != nil {
		return in, err
	}
	if err := osRename(tmp, a.confPath); err != nil {
		return in, err
	}
	if err := a.ctl.Reload(ctx); err != nil {
		return in, err
	}
	return in, nil
}

// zoneURLRoot 容器内可见的 zonefile 根（compose 将宿主 config/unbound 挂到该路径）。
const zoneURLRoot = "/etc/unbound-rendered/zones"

// writeZonefile 原子写入区文件并返回容器内引用路径。
func (a *confApplier) writeZonefile(zone string, records []unboundengine.ZoneRecord) (string, error) {
	dir := filepath.Join(filepath.Dir(a.confPath), "zones")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := strings.TrimSuffix(normalizeZoneName(zone), ".")
	if name == "" {
		name = "root"
	}
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, name) + ".zone"
	path := filepath.Join(dir, safe)
	tmp := path + ".candidate"
	if err := osWrite(tmp, string(unboundengine.RenderZonefile(zone, records))); err != nil {
		return "", err
	}
	if err := osRename(tmp, path); err != nil {
		return "", err
	}
	return zoneURLRoot + "/" + safe, nil
}

// forwardAddr 上游地址 → 转发目标（protocol=dot 时带 TLSName）。
func forwardAddr(u dnsmodule.Upstream, addr string) unboundengine.ForwardAddr {
	fa := unboundengine.ForwardAddr{Addr: unboundengine.NormalizeAddr(addr)}
	if u.Protocol == "dot" {
		fa.TLSName = u.TLSName
	}
	return fa
}

// splitCSV 逗号分隔串 → 去空切片（主服务器列表）。
func splitCSV(s string) []string {
	out := []string{}
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// normalizeZoneName 区域名补尾点（unbound auth-zone 名称需 FQDN）。
func normalizeZoneName(n string) string {
	n = strings.TrimSpace(n)
	if n == "" || strings.HasSuffix(n, ".") {
		return n
	}
	return n + "."
}

func fqdn(name, zone string) string {
	if strings.HasSuffix(name, ".") {
		return name
	}
	z := strings.TrimSuffix(zone, ".")
	// 名称已含区域后缀（用户直接填完整域名，如 work2046.com / www.work2046.com）→ 只补尾点，
	// 否则会拼出 work2046.com.work2046.com.（M3-012 现场问题：解析记录不生效）。
	if name == z || strings.HasSuffix(name, "."+z) {
		return name + "."
	}
	return name + "." + z
}

// ApplyDnsConf 实现 apigen.ServerInterface（POST /dns/conf/apply，§2.3 收口）。
func (a *confApplier) ApplyDnsConf(c *gin.Context) {
	in, err := a.apply(c.Request.Context())
	if err != nil {
		code := "APPLY_FAILED"
		status := http.StatusInternalServerError
		if err.Error() == "UNBOUND_DOWN" || err == unboundengine.ErrUnavailable {
			code, status = "UNBOUND_DOWN", http.StatusServiceUnavailable
		}
		problem.Write(c, status, "https://ipam.local/problems/"+code, code, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "applied": in})
}

var (
	osWrite       = func(path, content string) error { return os.WriteFile(path, []byte(content), 0o644) }
	osRename      = os.Rename
	jsonUnmarshal = func(raw json.RawMessage, v any) error { return json.Unmarshal(raw, v) }
)
