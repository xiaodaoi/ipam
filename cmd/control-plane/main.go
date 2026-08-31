package main

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
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
	"github.com/xiaodaoi/ipam/internal/module/dashboard"
	dhcpmodule "github.com/xiaodaoi/ipam/internal/module/dhcp"
	dnsmodule "github.com/xiaodaoi/ipam/internal/module/dns"
	dualstack "github.com/xiaodaoi/ipam/internal/module/dualstack"
	"github.com/xiaodaoi/ipam/internal/module/ipam"
	logq "github.com/xiaodaoi/ipam/internal/module/logquery"
	"github.com/xiaodaoi/ipam/internal/module/platform"
	"github.com/xiaodaoi/ipam/internal/pkg/migrator"
)

// logsAPI 组合用命名包装（logquery.Handler 经此提升方法）。
type logsAPI struct{ *logq.Handler }

// dashAPI 同上（规避 dashboard.Handler 与 platform.Handler 嵌入名冲突）。
type dashAPI struct{ *dashboard.Handler }

// dsAPI 双栈模板 handler 包装（dualstack.Handler 与 platform.Handler 同名冲突）。
type dsAPI struct{ *dualstack.Handler }

// dhcpAPI DHCP 选项与类匹配 handler 包装（dhcp.Handler 与 platform.Handler 字段名冲突）。
type dhcpAPI struct{ *dhcpmodule.Handler }

func pgLight(pool *pgxpool.Pool) func(context.Context) dashboard.Light {
	if pool == nil {
		return nil
	}
	return func(ctx context.Context) dashboard.Light {
		pctx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
		defer cancel()
		if err := pool.Ping(pctx); err != nil {
			return dashboard.LightDown
		}
		return dashboard.LightUp
	}
}

// chLight 仅在显式配置 IPAM_CH_ADDR 时点亮（PoC 内存模式报告 unknown）。
func chLight(st logq.Store) func(context.Context) dashboard.Light {
	if os.Getenv("IPAM_CH_ADDR") == "" {
		return nil
	}
	return func(ctx context.Context) dashboard.Light {
		ctx2, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
		defer cancel()
		if p, ok := st.(interface{ Ping(context.Context) error }); !ok || p.Ping(ctx2) != nil {
			return dashboard.LightDown
		}
		return dashboard.LightUp
	}
}

func tcpLight(name, raw string) func(context.Context) dashboard.Light {
	host, port, ok := hostPort(raw)
	if !ok {
		log.Printf("[lights] %s: 未配置探测地址 → unknown", name)
		return nil
	}
	addr := net.JoinHostPort(host, port)
	return func(ctx context.Context) dashboard.Light {
		d := net.Dialer{Timeout: 800 * time.Millisecond}
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			return dashboard.LightDown
		}
		_ = conn.Close()
		return dashboard.LightUp
	}
}

func unixLight(name, path string) func(context.Context) dashboard.Light {
	if path == "" {
		return nil
	}
	return func(ctx context.Context) dashboard.Light {
		d := net.Dialer{Timeout: 800 * time.Millisecond}
		conn, err := d.DialContext(ctx, "unix", path)
		if err != nil {
			return dashboard.LightDown
		}
		_ = conn.Close()
		return dashboard.LightUp
	}
}

// hostPort 从 http://host:port 或 host:port 提取拨号地址。
func hostPort(raw string) (host, port string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		host, port = u.Hostname(), u.Port()
	} else if h, p, err := net.SplitHostPort(raw); err == nil {
		host, port = h, p
	} else {
		host = raw
	}
	if port == "" {
		port = "80"
	}
	return host, port, true
}

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
	var keaCmd *keaengine.CtrlAgent
	var pool *pgxpool.Pool
	if dsn := os.Getenv("IPAM_DB_DSN"); dsn != "" {
		var err error
		pool, err = pgxpool.New(context.Background(), dsn)
		if err != nil {
			log.Fatalf("pg pool: %v", err)
		}
		if err := migrator.Run(context.Background(), pool, os.Getenv("IPAM_MIGRATIONS_DIR"), log.Printf); err != nil {
			log.Fatalf("migrations: %v", err)
		}
		orgStore = ipam.NewOrgRepo(pool)
		subRepo = ipam.NewSubnetRepo(pool)
		agent := keaengine.NewCtrlAgent(os.Getenv("IPAM_KEA_API"))
		keaDeploy = agent
		keaCmd = agent
	}

	orgH := ipam.NewOrgHandler(ipam.NewOrgService(orgStore))
	var applyDhcpFn func(context.Context) error
	notifyDhcp := func(ctx context.Context) error {
		if applyDhcpFn == nil {
			return nil
		}
		return applyDhcpFn(ctx)
	}
	subH := ipam.NewSubnetHandler(ipam.NewSubnetService(subRepo, orgStore, keaDeploy, notifyDhcp))

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
	// DHCP 选项/类（M2-016）+ host reservations 配置式下发（M3-007）：统一 config-set 收敛点
	var dhcpStore dhcpmodule.Store = dhcpmodule.NewMemStore()
	if pool != nil {
		dhcpStore = dhcpmodule.NewPgStore(pool)
	}
	applyDhcpFn = func(ctx context.Context) error {
		if keaCmd == nil {
			return errors.New("kea unavailable")
		}
		subs, err := subRepo.List(ctx, "", 0)
		if err != nil {
			return err
		}
		opts, err := dhcpStore.ListOptions(ctx)
		if err != nil {
			return err
		}
		classes, err := dhcpStore.ListClasses(ctx)
		if err != nil {
			return err
		}
		binds, err := resRepo.List(ctx)
		if err != nil {
			return err
		}
		cfg, err := keaengine.BuildConfigFull(subs, opts, classes, binds)
		if err != nil {
			return err
		}
		_, err = keaCmd.Command(ctx, "config-set", "dhcp4", cfg)
		if err != nil {
			log.Printf("[dhcp-apply] config-set: %v", err)
			return err
		}
		// M2-018/M2-019：存在 v6 子网时同步下发 Dhcp6（经 agent 转发 dhcp6 socket）；
		// v6 失败降级为软失败（log 记录，kea6 恢复后 PATCH 重触发全量补齐）——避免连坐 v4（M2-019 实测）
		cfg6, err6 := keaengine.BuildConfig6(subs)
		if err6 != nil {
			log.Printf("[dhcp6-apply] build: %v", err6)
		} else if s6, ok := cfg6.Dhcp6["subnet6"].([]map[string]any); ok && len(s6) > 0 {
			if _, err := keaCmd.Command(ctx, "config-set", "dhcp6", cfg6); err != nil {
				log.Printf("[dhcp6-apply] config-set（软失败）: %v", err)
			}
		}
		// config-write：运行态落盘（Kea 重启后配置不回滚到启动文件——M2-018 诊断发现）
		_, _ = keaCmd.Command(ctx, "config-write", "dhcp4", map[string]any{})
		_, _ = keaCmd.Command(ctx, "config-write", "dhcp6", map[string]any{})
		return nil
	}
	lease6Fn := func(ctx context.Context) ([]apigen.DhcpLease6, error) {
		if keaCmd == nil {
			return nil, nil
		}
		ls, err := keaCmd.Lease6List(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]apigen.DhcpLease6, 0, len(ls))
		for _, l := range ls {
			item := apigen.DhcpLease6{IpAddress: l.IPAddress, LeaseType: apigen.DhcpLease6LeaseType(l.LeaseType), Duid: l.DUID}
			if l.HWAddress != "" {
				item.HwAddress = &l.HWAddress
			}
			if l.HWSource != 0 {
				hs := l.HWSource
				item.HwAddrSource = &hs
			}
			if v := int(l.IAID); v > 0 {
				item.Iaid = &v
			}
			if pn := int(l.PrefixLen); pn > 0 {
				item.PrefixLen = &pn
			}
			if ct := int(l.CLTT); ct > 0 {
				item.Cltt = &ct
			}
			if vl := int(l.ValidLifetime); vl > 0 {
				item.ValidLifetime = &vl
			}
			out = append(out, item)
		}
		return out, nil
	}
	dhcpH := dhcpmodule.NewHandler(dhcpStore, applyDhcpFn, lease6Fn)

	ledgerH := ipam.NewLedgerHandler(ipam.NewLedgerService(ledgerSrc, resRepo, subRepo, notifyDhcp))
	var assetRepo ipam.AssetRepo = ipam.NewMemAssetRepo()
	if pool != nil {
		assetRepo = ipam.NewPgAssetRepo(pool)
	}
	assetH := ipam.NewAssetHandler(ipam.NewAssetService(assetRepo, orgStore))

	// 日志检索（M4-002，FR-E-04）：配置 IPAM_CH_ADDR 走 ClickHouse，否则内存 PoC；
	// 组织展开：PG 子树（org_group.path）或内存节点遍历。
	var logStore logq.Store = logq.NewMemStore()
	if chAddr := os.Getenv("IPAM_CH_ADDR"); chAddr != "" {
		chDB := os.Getenv("IPAM_CH_DB")
		if chDB == "" {
			chDB = "ipam"
		}
		st, err := logq.OpenChStore(logq.ChConfig{
			Addr: chAddr, DB: chDB,
			User: os.Getenv("IPAM_CH_USER"), Password: os.Getenv("IPAM_CH_PASSWORD"),
		})
		if err != nil {
			log.Fatalf("ch store: %v", err)
		}
		logStore = st
	}
	var logExpander logq.OrgExpander = ipam.NewMemOrgExpander(orgStore, subRepo, assetRepo)
	if pool != nil {
		logExpander = logq.NewPgOrgExpander(pool)
	}
	logH := logq.NewHandler(logq.NewService(logStore, logExpander))

	var auditRepo logq.AuditStore = logq.NewMemAuditStore()
	if pool != nil {
		auditRepo = logq.NewPgAuditStore(pool)
	}
	auditH := logq.NewAuditHandler(auditRepo)

	// 仪表盘聚合（M4-004，F-01）：日志口径走 logquery.Store，子网/联动绑定按 PG/Mem 装配；
	// 健康灯仅对已配置探测目标的组件点亮（PoC 模式全 unknown）。
	var bindSrc dashboard.BindingSource
	if pool != nil {
		bindSrc = func(ctx context.Context) ([]ipam.LedgerBinding, error) {
			return ipam.LoadLedgerBindings(ctx, pool)
		}
	}
	var subSrc dashboard.SubnetSource = func(ctx context.Context) []ipam.Subnet {
		subs, _ := subRepo.List(ctx, "", 0)
		return subs
	}
	lights := dashboard.Lights{
		Postgres:   pgLight(pool),
		ClickHouse: chLight(logStore),
		Kea:        tcpLight("kea", os.Getenv("IPAM_KEA_API")),
		Unbound:    unixLight("unbound", "/run/ipam/unbound-ctl.sock"),
	}
	dashH := dashboard.NewHandler(dashboard.NewService(logStore, subSrc, bindSrc, lights))

	// 双栈绑定模板（M2-012，§4.3）：daemon 联动匹配的模板数据源
	var dsStore dualstack.Store = dualstack.NewMemStore()
	if pool != nil {
		dsStore = dualstack.NewPgStore(pool)
	}
	dsH := dualstack.NewHandler(dsStore)

	// 用户与角色（M5-004）：登录校验与用户管理共用；空表引导播种 admin
	var userStore platform.UserStore = platform.NewMemUserStore()
	if pool != nil {
		userStore = platform.NewPgUserStore(pool)
	}
	if err := platform.EnsureBootstrap(context.Background(), userStore); err != nil {
		log.Printf("user bootstrap: %v", err)
	}
	bl := platform.NewTokenBlacklist() // M5-011：登出黑名单（AuthHandler/RBAC 共用）
	if pool != nil {
		if err := bl.AttachDB(context.Background(), pool); err != nil {
			log.Printf("[bl-load] 黑名单持久化加载失败（降级内存）: %v", err)
		}
	}

	var upRepo dnsmodule.UpstreamRepo = dnsmodule.NewMemUpstreamRepo()
	var unboundCtl dnsmodule.UnboundController = unboundengine.ExecController{Conf: os.Getenv("IPAM_UNBOUND_CONF")}
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
	var blRepo dnsmodule.BlocklistRepo // 声明提前（M2-031 policyView 闭包捕获，赋值在下方）
	policyViewFn := func(ctx context.Context, clientIP string) (string, bool) {
		groups, err := blRepo.ListPolicyGroups(ctx)
		if err != nil {
			return "", false
		}
		return dnsmodule.MatchPolicyView(groups, clientIP)
	}
	dnsH := dnsmodule.NewDnsHandler(dnsSvc, policyViewFn)
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
	blRepo = dnsmodule.NewMemBlocklistRepo()
	if pool != nil {
		blRepo = dnsmodule.NewPgBlocklistRepo(pool)
	}
	blSvc := dnsmodule.NewBlocklistService(blRepo, nil, unboundCtl, "/var/lib/ipam/rpz")
	// M2-033：启动重放封禁 local_zone（unbound 重启后运行时态恢复）
	go func() {
		if err := blSvc.ReplayAll(context.Background()); err != nil {
			log.Printf("[bl-replay] %v", err)
		}
	}()
	blH := dnsmodule.NewBlocklistHandler(blSvc)
	var settingsRepo dnsmodule.SettingsRepo = dnsmodule.NewMemSettingsRepo()
	if pool != nil {
		settingsRepo = dnsmodule.NewPgSettingsRepo(pool)
	}
	applier := &confApplier{
		pool: pool, upRepo: upRepo, frRepo: frRepo, zoneRepo: zoneRepo,
		blRepo: blRepo, settings: settingsRepo, ctl: unboundCtl,
		confPath: "/etc/unbound-rendered/unbound-rendered.conf",
	}
	settingsSvc := dnsmodule.NewSettingsService(settingsRepo, unboundCtl, "/etc/unbound-rendered/unbound-rendered.conf", func(ctx context.Context) error {
		_, err := applier.apply(ctx)
		if err != nil {
			log.Printf("[settings-notify] %v", err)
		}
		return err
	})
	settingsH := dnsmodule.NewSettingsHandler(settingsSvc, settingsRepo)
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
	// 注：logsAPI 包装让组合字段名不同于 platform.Handler，避免同名冲突。
	logs := logsAPI{logH}
	full := struct {
		*platform.Handler
		*ipam.OrgHandler
		*ipam.SubnetHandler
		*ipam.LedgerHandler
		*ipam.AssetHandler
		*logsAPI
		*logq.AuditHandler
		*dashAPI
		*dsAPI
		*platform.AuthHandler
		*platform.UserHandler
		*dhcpAPI
		*dnsmodule.DnsHandler
		*dnsmodule.ForwardHandler
		*dnsmodule.ZoneHandler
		*dnsmodule.BlocklistHandler
		*dnsmodule.SettingsHandler
		*confApplier
	}{h, orgH, subH, ledgerH, assetH, &logs, auditH, &dashAPI{dashH}, &dsAPI{dsH}, platform.NewAuthHandler(userStore, bl), platform.NewUserHandler(userStore), &dhcpAPI{dhcpH}, dnsH, fwdH, zoneH, blH, settingsH, applier}
	// RBAC 写权限拦截（M5-003）先于审计：被 403 的请求不入账。
	// 操作审计（M4-003+M5-002）：actor 从 JWT claims 解析（human/bot 区分 §12.3）。
	r.Use(platform.NewRBACMiddleware(userStore, bl)) // M5-010/M5-011：认证+授权+登出吊销
	r.Use(logq.NewAuditRecorder(auditRepo, platform.JWTActorProvider))
	// spec servers.url=/api/v1 → 统一前缀注册
	apigen.RegisterHandlersWithOptions(r, full, apigen.GinServerOptions{BaseURL: "/api/v1"})

	dist, err := webuiFS()
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

// webuiFS 开发旁路：IPAM_WEBUI_DIR 指向磁盘 dist 时直接读盘（前端改完免镜像重建）；
// 未设置走 go:embed（§13.3 单二进制交付不变）。
func webuiFS() (fs.FS, error) {
	if dir := os.Getenv("IPAM_WEBUI_DIR"); dir != "" {
		return os.DirFS(dir), nil
	}
	return webui.FS()
}

func main() {
	addr := os.Getenv("IPAM_HTTP_ADDR")
	if addr == "" {
		addr = ":8443"
	}
	log.Printf("control-plane %s listening on %s", version, addr)
	if err := newEngine(version).Run(addr); err != nil {
		log.Fatal(err)
	}
}
