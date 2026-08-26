package main

import (
	"context"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/cmd/control-plane/webui"
	keaengine "github.com/xiaodaoi/ipam/internal/engine/kea"
	unboundengine "github.com/xiaodaoi/ipam/internal/engine/unbound"
	dnsmodule "github.com/xiaodaoi/ipam/internal/module/dns"
	"github.com/xiaodaoi/ipam/internal/module/ipam"
	"github.com/xiaodaoi/ipam/internal/module/platform"
)

// newEngine 装配完整路由：/api/v1 业务路由 + webui embed + SPA fallback（§13.3）。
func newEngine(version string) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	h := platform.NewHandler(version)

	// 仓储装配：IPAM_DB_DSN 存在则走 PG（生产/compose），否则内存 PoC。
	var orgStore ipam.OrgStore = ipam.NewMemOrgStore()
	var subRepo ipam.SubnetRepo = ipam.NewMemSubnetRepo()
	var keaDeploy ipam.KeaDeployer = ipam.NewNoopKea()
	var pool *pgxpool.Pool
	if dsn := os.Getenv("IPAM_DB_DSN"); dsn != "" {
		var err error
		pool, err = pgxpool.New(context.Background(), dsn)
		if err != nil {
			log.Fatalf("pg pool: %v", err)
		}
		orgStore = ipam.NewOrgRepo(pool)
		subRepo = ipam.NewSubnetRepo(pool)
		keaDeploy = keaengine.NewCtrlAgent(os.Getenv("IPAM_KEA_API"))
	}

	orgH := ipam.NewOrgHandler(ipam.NewOrgService(orgStore))
	subH := ipam.NewSubnetHandler(ipam.NewSubnetService(subRepo, orgStore, keaDeploy))

	var resRepo ipam.ReservationRepo = ipam.NewMemReservationRepo()
	if pool != nil {
		resRepo = ipam.NewPgReservationRepo(pool)
	}
	ledgerSrc := func(ctx context.Context) ipam.LedgerSource {
		bindings := []ipam.LedgerBinding{}
		if pool != nil {
			if bs, err := ipam.LoadLedgerBindings(ctx, pool); err == nil {
				bindings = bs
			}
		}
		reservations, _ := resRepo.List(ctx)
		subs, _ := subRepo.List(ctx, "", 0)
		assets := map[string]ipam.Asset{}
		if pool != nil {
			if list, err := ipam.NewPgAssetRepo(pool).List(ctx, "", ""); err == nil {
				for _, a := range list {
					assets[a.MAC] = a
				}
			}
		}
		return ipam.LedgerSource{
			Bindings: bindings, Reservations: reservations,
			Assets: assets, Subnets: subs,
		}
	}
	ledgerH := ipam.NewLedgerHandler(ipam.NewLedgerService(ledgerSrc, resRepo, keaDeploy, subRepo))
	var assetRepo ipam.AssetRepo = ipam.NewMemAssetRepo()
	if pool != nil {
		assetRepo = ipam.NewPgAssetRepo(pool)
	}
	assetH := ipam.NewAssetHandler(ipam.NewAssetService(assetRepo, orgStore))
	var upRepo dnsmodule.UpstreamRepo = dnsmodule.NewMemUpstreamRepo()
	var unboundCtl dnsmodule.UnboundController = unboundengine.ExecController{}
	if pool != nil {
		upRepo = dnsmodule.NewPgUpstreamRepo(pool)
	}
	prober := dnsmodule.NewProber(15*time.Second, unboundengine.DialProbe)
	dnsSvc := dnsmodule.NewService(upRepo, prober, unboundCtl)
	go prober.Run(context.Background(), func() []dnsmodule.Upstream {
		list, err := upRepo.List(context.Background())
		if err != nil {
			return nil
		}
		return list
	})
	dnsH := dnsmodule.NewDnsHandler(dnsSvc)
	var frRepo dnsmodule.ForwardRuleRepo = dnsmodule.NewMemForwardRuleRepo()
	if pool != nil {
		frRepo = dnsmodule.NewPgForwardRuleRepo(pool)
	}
	fwdSvc := dnsmodule.NewForwardService(frRepo, upRepo, unboundCtl)
	fwdH := dnsmodule.NewForwardHandler(fwdSvc)
	var zoneRepo dnsmodule.ZoneRepo = dnsmodule.NewMemZoneRepo()
	if pool != nil {
		zoneRepo = dnsmodule.NewPgZoneRepo(pool)
	}
	zoneSvc := dnsmodule.NewZoneService(zoneRepo, unboundCtl)
	var blRepo dnsmodule.BlocklistRepo = dnsmodule.NewMemBlocklistRepo()
	if pool != nil {
		blRepo = dnsmodule.NewPgBlocklistRepo(pool)
	}
	blSvc := dnsmodule.NewBlocklistService(blRepo, nil, unboundCtl, "/var/lib/ipam/rpz")
	blH := dnsmodule.NewBlocklistHandler(blSvc)
	var settingsRepo dnsmodule.SettingsRepo = dnsmodule.NewMemSettingsRepo()
	if pool != nil {
		settingsRepo = dnsmodule.NewPgSettingsRepo(pool)
	}
	settingsSvc := dnsmodule.NewSettingsService(settingsRepo, unboundCtl, "/etc/unbound/unbound.conf")
	settingsH := dnsmodule.NewSettingsHandler(settingsSvc, settingsRepo)
	applier := &confApplier{
		pool: pool, upRepo: upRepo, frRepo: frRepo, zoneRepo: zoneRepo,
		blRepo: blRepo, settings: settingsRepo, ctl: unboundCtl,
		confPath: "/etc/unbound/unbound.conf",
	}
	registerConfRoutes(r, applier)
	zoneH := dnsmodule.NewZoneHandler(zoneSvc, func(ctx context.Context, zoneName string) []dnsmodule.LinkedRecord {
		if pool == nil {
			return nil
		}
		bindings, err := ipam.LoadLedgerBindings(ctx, pool)
		if err != nil {
			return nil
		}
		out := []dnsmodule.LinkedRecord{}
		for _, b := range bindings {
			name := "host-" + strings.ReplaceAll(strings.ToLower(b.MAC), ":", "-") + "." + strings.TrimPrefix(zoneName, ".")
			if b.IPv4 != "" {
				out = append(out, dnsmodule.LinkedRecord{Name: name, RecType: "A", Rdata: b.IPv4, MAC: b.MAC})
			}
			if b.IPv6 != "" {
				out = append(out, dnsmodule.LinkedRecord{Name: name, RecType: "AAAA", Rdata: b.IPv6, MAC: b.MAC})
			}
		}
		return out
	})
	// 组合各域 handler 共同实现 ServerInterface（Go 嵌入提升；新增域在此扩展）
	full := struct {
		*platform.Handler
		*ipam.OrgHandler
		*ipam.SubnetHandler
		*ipam.LedgerHandler
		*ipam.AssetHandler
		*dnsmodule.DnsHandler
		*dnsmodule.ForwardHandler
		*dnsmodule.ZoneHandler
		*dnsmodule.BlocklistHandler
		*dnsmodule.SettingsHandler
	}{h, orgH, subH, ledgerH, assetH, dnsH, fwdH, zoneH, blH, settingsH}
	// spec servers.url=/api/v1 → 统一前缀注册
	apigen.RegisterHandlersWithOptions(r, full, apigen.GinServerOptions{BaseURL: "/api/v1"})

	dist, err := webui.FS()
	if err != nil {
		log.Fatalf("webui fs: %v", err)
	}
	serveStatic := func(c *gin.Context, name string) {
		data, rerr := fs.ReadFile(dist, name)
		if rerr != nil {
			platform.WriteProblem(c, http.StatusInternalServerError,
				"https://ipam.local/problems/internal", "WEBUI_ASSET_MISSING", "静态资源读取失败")
			return
		}
		ct := mime.TypeByExtension(filepath.Ext(name))
		if ct == "" {
			ct = "application/octet-stream"
		}
		c.Data(http.StatusOK, ct, data)
	}
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api/") {
			// API 未匹配路由统一返回 RFC 9457，便于机器判读（§12.2）
			platform.WriteProblem(c, http.StatusNotFound,
				"https://ipam.local/problems/not-found", "API_ROUTE_NOT_FOUND", "未知 API 路由")
			return
		}
		// 静态资源命中则回文件；否则回 index.html（history 路由 SPA fallback）
		target := strings.TrimPrefix(path, "/")
		if target == "" {
			target = "index.html"
		}
		if _, oerr := fs.Stat(dist, target); oerr == nil && !strings.HasSuffix(target, "/") {
			serveStatic(c, target)
			return
		}
		serveStatic(c, "index.html")
	})
	return r
}

// version 构建期注入：-ldflags "-X main.version=x.y.z"
var version = "0.1.0-dev"

func main() {
	addr := ":8443"
	log.Printf("control-plane %s listening on %s", version, addr)
	if err := newEngine(version).Run(addr); err != nil {
		log.Fatal(err)
	}
}
