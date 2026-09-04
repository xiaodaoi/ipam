package main

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
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
			in.DefaultFwd = append(in.DefaultFwd, unboundengine.NormalizeAddr(addr))
		}
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
			rc.Addrs = append(rc.Addrs, normalizeAddrs(u.Addrs)...)
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
		if z.Kind != "auth" || !z.Enabled {
			continue
		}
		records, err := a.zoneRepo.ListRecords(ctx, z.ID)
		if err != nil {
			continue
		}
		az := unboundengine.AuthZoneConf{Name: z.Name}
		for _, rec := range records {
			if rec.Enabled {
				az.Records = append(az.Records, unboundengine.ZoneRecord{
					Name: fqdn(rec.Name, z.Name), Type: rec.RecType, TTL: rec.TTL, Data: rec.Rdata,
				})
			}
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

func normalizeAddrs(addrs []string) []string {
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		host, port, err := net.SplitHostPort(unboundengine.NormalizeAddr(a))
		if err != nil {
			continue
		}
		out = append(out, host+"@"+port)
	}
	return out
}

func fqdn(name, zone string) string {
	if strings.HasSuffix(name, ".") {
		return name
	}
	return name + "." + strings.TrimPrefix(zone, ".")
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
