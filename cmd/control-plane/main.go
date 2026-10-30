package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/cmd/control-plane/webui"
	keaengine "github.com/xiaodaoi/ipam/internal/engine/kea"
	unboundengine "github.com/xiaodaoi/ipam/internal/engine/unbound"
	backupmodule "github.com/xiaodaoi/ipam/internal/module/backup"
	"github.com/xiaodaoi/ipam/internal/module/coherence"
	"github.com/xiaodaoi/ipam/internal/module/dashboard"
	dhcpmodule "github.com/xiaodaoi/ipam/internal/module/dhcp"
	dnsmodule "github.com/xiaodaoi/ipam/internal/module/dns"
	dualstack "github.com/xiaodaoi/ipam/internal/module/dualstack"
	"github.com/xiaodaoi/ipam/internal/module/ipam"
	"github.com/xiaodaoi/ipam/internal/module/logexport"
	"github.com/xiaodaoi/ipam/internal/module/logmanager"
	logq "github.com/xiaodaoi/ipam/internal/module/logquery"
	"github.com/xiaodaoi/ipam/internal/module/platform"
	"github.com/xiaodaoi/ipam/internal/module/utilization"
	"github.com/xiaodaoi/ipam/internal/pkg/hostmetrics"
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
// rootCtx 为信号感知的根上下文（M5-032）：所有后台循环随其取消而退出。
func newEngine(version string, rootCtx context.Context) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(platform.SecurityHeaders())
	rateLimit := 600
	if v := os.Getenv("IPAM_RATE_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			rateLimit = n
		}
	}
	r.Use(platform.APIRateLimit(rateLimit))

	h := platform.NewHandler(version)
	// 宿主资源采集：容器经只读挂载读宿主 /proc 等（env 未设则回落本机 /proc、/）。
	hostCol := hostmetrics.New(
		os.Getenv("IPAM_HOST_PROC"), os.Getenv("IPAM_HOST_SYS"), os.Getenv("IPAM_HOST_ROOT"))
	h.SetHostMetrics(hostCol)

	// 仓储装配：IPAM_DB_DSN 存在则走 PG（生产/compose），否则内存 PoC。
	var orgStore ipam.OrgStore = ipam.NewMemOrgStore()
	var subRepo ipam.SubnetRepo = ipam.NewMemSubnetRepo()
	var keaDeploy ipam.KeaDeployer = ipam.NewNoopKea()
	var keaCmd *keaengine.CtrlAgent
	var pool *pgxpool.Pool
	if dsn := os.Getenv("IPAM_DB_DSN"); dsn != "" {
		// M5-032：显式池配置——零配置下死/闲连接不剔除，长运行后登录间歇 500。
		cfg, err := pgxpool.ParseConfig(dsn)
		if err != nil {
			log.Fatalf("pg pool config: %v", err)
		}
		cfg.MaxConns = 16
		cfg.MinConns = 1
		cfg.MaxConnLifetime = 30 * time.Minute
		cfg.MaxConnIdleTime = 5 * time.Minute
		cfg.HealthCheckPeriod = time.Minute
		pool, err = pgxpool.NewWithConfig(rootCtx, cfg)
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
	var metaRepo ipam.LedgerMetaRepo = ipam.NewMemLedgerMetaRepo()
	if pool != nil {
		resRepo = ipam.NewPgReservationRepo(pool)
		metaRepo = ipam.NewPgLedgerMetaRepo(pool)
	}
	// 子网利用率采样（M6-010B）：默认每 5 分钟，保留 90 天
	var utilRepo utilization.Repo = utilization.NewMemRepo()
	if pool != nil {
		utilRepo = utilization.NewPgRepo(pool)
	}
	utilSvc := utilization.NewService(utilRepo, utilization.Sources{
		Subnets:      func(ctx context.Context) ([]ipam.Subnet, error) { return subRepo.List(ctx, "", 0) },
		Reservations: func(ctx context.Context) ([]ipam.Reservation, error) { return resRepo.List(ctx) },
		Bindings: func(ctx context.Context) ([]ipam.LedgerBinding, error) {
			if pool == nil {
				return nil, nil
			}
			return ipam.LoadLedgerBindings(ctx, pool)
		},
	})
	utilInterval := 5 * time.Minute
	if v := os.Getenv("IPAM_UTIL_SAMPLE_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= time.Minute {
			utilInterval = d
		}
	}
	go utilSvc.Run(rootCtx, utilInterval, log.Printf)
	utilH := utilization.NewUtilHandler(utilSvc)

	ledgerSrc := func(ctx context.Context) ipam.LedgerSource {
		bindings := []ipam.LedgerBinding{}
		if pool != nil {
			if bs, err := ipam.LoadLedgerBindings(ctx, pool); err == nil {
				bindings = bs
			}
		}
		reservations, _ := resRepo.List(ctx)
		subs, _ := subRepo.List(ctx, "", 0)
		meta := map[string]ipam.LedgerMeta{}
		if pool != nil {
			if mm, err := ipam.LoadLedgerMeta(ctx, pool); err == nil {
				meta = mm
			}
		}
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
			Assets: assets, Meta: meta, Subnets: subs,
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
	// 启动 DHCP 收敛（M3-012 现场教训：kea 容器重建后运行态配置丢失，直到下次子网变更才恢复）——
	// kea 可能未就绪，重试 6×5s；与 unbound 启动收敛对偶。
	go func() {
		for attempt := 1; attempt <= 6; attempt++ {
			if err := applyDhcpFn(rootCtx); err == nil {
				log.Printf("[dhcp-apply] startup converge ok (attempt %d)", attempt)
				return
			} else if attempt < 6 {
				log.Printf("[dhcp-apply] startup attempt %d failed: %v", attempt, err)
				select {
				case <-rootCtx.Done():
					return
				case <-time.After(5 * time.Second):
				}
			} else {
				log.Printf("[dhcp-apply] startup converge failed after retries: %v", err)
			}
		}
	}()

	// Kea v4 租约→coherence_binding 同步（30s 轮询）：台账在线态/联动记录的数据源
	// （依赖 dhcp4 的 lease_cmds hook，M3-012 补齐；此前写入方缺失导致台账无在线态）。
	if pool != nil && keaCmd != nil {
		coherence.StartLease4SyncLoop(rootCtx, pool, os.Getenv("IPAM_KEA_API"), 30*time.Second)
		coherence.StartLease6SyncLoop(rootCtx, pool, os.Getenv("IPAM_KEA_API"), 30*time.Second)
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
	var leaseAgent dhcpmodule.LeaseAgent
	if keaCmd != nil {
		leaseAgent = keaCmd
	}
	leaseH := dhcpmodule.NewLeaseHandler(leaseAgent)

	ledgerH := ipam.NewLedgerHandler(ipam.NewLedgerService(ledgerSrc, resRepo, subRepo, notifyDhcp))
	metaH := ipam.NewLedgerMetaHandler(metaRepo)
	var assetRepo ipam.AssetRepo = ipam.NewMemAssetRepo()
	if pool != nil {
		assetRepo = ipam.NewPgAssetRepo(pool)
	}
	assetH := ipam.NewAssetHandler(ipam.NewAssetService(assetRepo, orgStore))
	assetH.SetBindingSource(func(ctx context.Context) ([]ipam.LedgerBinding, error) {
		if pool == nil {
			return nil, nil
		}
		return ipam.LoadLedgerBindings(ctx, pool)
	})

	// 日志检索（M4-002，FR-E-04）：配置 IPAM_CH_ADDR 走 ClickHouse，否则内存 PoC；
	// 组织展开：PG 子树（org_group.path）或内存节点遍历。
	var logStore logq.Store = logq.NewMemStore()
	var chCfg logq.ChConfig
	if chAddr := os.Getenv("IPAM_CH_ADDR"); chAddr != "" {
		chDB := os.Getenv("IPAM_CH_DB")
		if chDB == "" {
			chDB = "ipam"
		}
		chCfg = logq.ChConfig{
			Addr: chAddr, DB: chDB,
			User: os.Getenv("IPAM_CH_USER"), Password: os.Getenv("IPAM_CH_PASSWORD"),
		}
		st, err := logq.OpenChStore(chCfg)
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
	var secStore platform.SecuritySettingsStore = platform.NewMemSecuritySettingsStore()
	if pool != nil {
		secStore = platform.NewPgSecuritySettingsStore(pool)
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
	go prober.Run(rootCtx, func() []dnsmodule.Upstream {
		list, err := upRepo.List(rootCtx)
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
		if err := blSvc.ReplayAll(rootCtx); err != nil {
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
	dnsSvc.RulesSync = fwdSvc.SyncRules
	fwdSvc.FileApply = func(ctx context.Context) error {
		_, err := applier.apply(ctx)
		return err
	}
	zoneSvc.ApplyConf = func(ctx context.Context) error {
		_, err := applier.apply(ctx)
		return err
	}
	settingsH := dnsmodule.NewSettingsHandler(settingsSvc, settingsRepo)

	// 启动收敛（M3-011 事故教训：unbound 容器重建后运行态转发区丢失）——
	// 全量渲染+checkconf+reload 把文件与运行态都收敛到 DB 现状；unbound 未就绪则重试。
	go func() {
		for attempt := 1; attempt <= 6; attempt++ {
			if _, err := applier.apply(rootCtx); err == nil {
				log.Printf("[conf-apply] startup converge ok (attempt %d)", attempt)
				return
			} else if attempt < 6 {
				log.Printf("[conf-apply] startup attempt %d failed: %v", attempt, err)
				select {
				case <-rootCtx.Done():
					return
				case <-time.After(5 * time.Second):
				}
			} else {
				log.Printf("[conf-apply] startup converge failed after retries: %v", err)
			}
		}
	}()
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
	var roleStore platform.RoleStore = platform.NewMemRoleStore()
	if pool != nil {
		roleStore = platform.NewPgRoleStore(pool)
		if rs, err := roleStore.List(context.Background()); err == nil {
			for _, r := range rs {
				if !r.Builtin {
					platform.RegisterRole(r.Name)
				}
			}
		}
	}
	rolesH := platform.NewRolesHandler(roleStore)
	var webuiRepo platform.WebuiRepo = platform.NewMemWebuiRepo()
	if pool != nil {
		webuiRepo = platform.NewPgWebuiRepo(pool)
	}
	webuiH := platform.NewWebuiHandler(webuiRepo)

	var logSettingsRepo logmanager.SettingsRepo = logmanager.NewMemSettingsRepo()
	var logArchiveRepo logmanager.ArchiveRepo = logmanager.NewMemArchiveRepo()
	if pool != nil {
		logSettingsRepo = logmanager.NewPgSettingsRepo(pool)
		logArchiveRepo = logmanager.NewPgArchiveRepo(pool)
	}
	var chAdmin *logmanager.ChAdmin
	if chCfg.Addr != "" {
		ca, err := logmanager.OpenChAdmin(logmanager.ChConfig{
			Addr: chCfg.Addr, DB: chCfg.DB, User: chCfg.User, Password: chCfg.Password,
			HTTPAddr: os.Getenv("IPAM_CH_HTTP"),
		})
		if err != nil {
			log.Fatalf("ch admin: %v", err)
		}
		chAdmin = ca
	}
	logExportDir := os.Getenv("IPAM_LOG_EXPORT_DIR")
	if logExportDir == "" {
		logExportDir = "/srv/log-exports"
	}
	if err := os.MkdirAll(logExportDir, 0o755); err != nil {
		log.Printf("log export dir %s: %v", logExportDir, err)
	}
	logMgrH := logmanager.NewLogHandler(logSettingsRepo, logArchiveRepo, chAdmin, logExportDir)

	// 备份恢复（M6-002）：PG + 配置 + ClickHouse 日志 Parquet
	backupDir := os.Getenv("IPAM_BACKUP_DIR")
	if backupDir == "" {
		backupDir = filepath.Join(logExportDir, "backups")
	}
	configDirs := []string{"/etc/unbound-ctl"}
	if v := os.Getenv("IPAM_BACKUP_CONFIG_DIRS"); v != "" {
		configDirs = strings.Split(v, ",")
	}
	var backupRepo backupmodule.Repo = backupmodule.NewMemRepo()
	var backupSettingsRepo backupmodule.SettingsRepo = backupmodule.NewMemSettings()
	if pool != nil {
		backupRepo = backupmodule.NewPgRepo(pool)
		backupSettingsRepo = backupmodule.NewPgSettings(pool)
	}
	var pgRunner backupmodule.PgRunner
	if dsn := os.Getenv("IPAM_DB_DSN"); dsn != "" {
		pr, err := backupmodule.NewPgCommandRunner(dsn)
		if err != nil {
			log.Printf("[backup] pg runner 未启用: %v", err)
		} else {
			pgRunner = pr
		}
	}
	var backupLogs backupmodule.LogStore
	if chAdmin != nil {
		backupLogs = chAdmin
	}
	backupSvc := backupmodule.NewService(backupRepo, backupSettingsRepo, pgRunner, backupLogs, backupDir, configDirs)
	licenseH := platform.NewLicenseHandler(platform.NewLicenseService(
		os.Getenv("IPAM_LICENSE_FILE"), os.Getenv("IPAM_LICENSE_PUBKEY")))

	// 日志外发/采样（M6-008）：启动时渲染 Vector 配置，保存后热加载
	var exportRepo logexport.Repo = logexport.NewMemRepo()
	if pool != nil {
		exportRepo = logexport.NewPgRepo(pool)
	}
	vectorTemplate := os.Getenv("IPAM_VECTOR_TEMPLATE")
	if vectorTemplate == "" {
		vectorTemplate = "/etc/vector-template/vector.toml"
	}
	vectorConfig := os.Getenv("IPAM_VECTOR_CONFIG")
	if vectorConfig == "" {
		vectorConfig = "/etc/vector-config/vector.toml"
	}
	exportSvc := logexport.NewService(exportRepo, vectorTemplate, vectorConfig)
	if err := exportSvc.Apply(rootCtx); err != nil {
		log.Printf("[log-export] 渲染 Vector 配置失败（外发可能未生效）: %v", err)
	}
	exportH := logexport.NewLogExportHandler(exportSvc)
	backupH := backupmodule.NewAPIHandler(backupSvc, backupSettingsRepo)
	permLookup := func(ctx context.Context, role string) ([]string, bool) {
		if pool == nil {
			return nil, false
		}
		var perms string
		if err := pool.QueryRow(ctx, `SELECT permissions FROM roles WHERE name = $1`, role).Scan(&perms); err != nil {
			return nil, false
		}
		var out []string
		_ = json.Unmarshal([]byte(perms), &out)
		return out, true
	}
	full := struct {
		*platform.Handler
		*ipam.OrgHandler
		*ipam.SubnetHandler
		*ipam.LedgerHandler
		*ipam.LedgerMetaHandler
		*ipam.AssetHandler
		*logsAPI
		*logq.AuditHandler
		*dashAPI
		*dsAPI
		*platform.AuthHandler
		*platform.UserHandler
		*dhcpAPI
		*dhcpmodule.LeaseHandler
		*utilization.UtilHandler
		*dnsmodule.DnsHandler
		*dnsmodule.ForwardHandler
		*dnsmodule.ZoneHandler
		*dnsmodule.BlocklistHandler
		*dnsmodule.SettingsHandler
		*platform.RolesHandler
		*platform.WebuiHandler
		*platform.SecuritySettingsHandler
		*platform.LicenseHandler
		*logmanager.LogHandler
		*logexport.LogExportHandler
		*backupmodule.APIHandler
		*confApplier
	}{h, orgH, subH, ledgerH, metaH, assetH, &logs, auditH, &dashAPI{dashH}, &dsAPI{dsH}, platform.NewAuthHandler(userStore, bl, permLookup, secStore), platform.NewUserHandler(userStore, secStore), &dhcpAPI{dhcpH}, leaseH, utilH, dnsH, fwdH, zoneH, blH, settingsH, rolesH, webuiH, platform.NewSecuritySettingsHandler(secStore), licenseH, logMgrH, exportH, backupH, applier}
	// Prometheus 指标（M6-005）：放在 RBAC 之前供抓取；中间件只统计其后的 /api/v1 路由。
	var logStatsFn func(context.Context) (logmanager.LogStats, error)
	if chAdmin != nil {
		logStatsFn = chAdmin.LogStats
	}
	metricsH := platform.NewMetricsHandler(platform.MetricsSources{
		Version:     version,
		Utilization: utilSvc.Latest,
		LogStats:    logStatsFn,
		Postgres: func(ctx context.Context) error {
			if pool == nil {
				return nil
			}
			return pool.Ping(ctx)
		},
		ClickHouse: func(ctx context.Context) error {
			if chAdmin == nil {
				return nil
			}
			return chAdmin.Ping(ctx)
		},
		Kea: func(ctx context.Context) error {
			if keaCmd == nil {
				return nil
			}
			_, err := keaCmd.Command(ctx, "version-get", "dhcp4", nil)
			return err
		},
		Host:        hostCol,
		TLSCertFile: os.Getenv("IPAM_TLS_CERT"),
	})
	r.GET("/metrics", metricsH.Serve)
	// OpenAPI 在线事实源（M6-012）：无需令牌，仅暴露接口契约，不含数据。
	if spec, err := apigen.GetSpec(); err == nil {
		r.GET("/openapi.json", func(c *gin.Context) {
			c.JSON(http.StatusOK, spec)
		})
	}
	r.Use(metricsH.Middleware())

	// 健康探针放在 RBAC 之前：容器编排/负载均衡无需令牌即可探活（M6-001）。
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/readyz", func(c *gin.Context) {
		if pool != nil {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
			defer cancel()
			if err := pool.Ping(ctx); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not-ready", "postgres": "down"})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
	// RBAC 写权限拦截（M5-003）先于审计：被 403 的请求不入账。
	// 操作审计（M4-003+M5-002）：actor 从 JWT claims 解析（human/bot 区分 §12.3）。
	r.Use(platform.NewRBACMiddleware(userStore, bl, permLookup)) // M5-003/M5-010/M5-011/M2-035：认证+授权+吊销+域权限
	r.Use(logq.NewAuditRecorder(auditRepo, platform.JWTActorProvider))
	// spec servers.url=/api/v1 → 统一前缀注册
	apigen.RegisterHandlersWithOptions(r, full, apigen.GinServerOptions{BaseURL: "/api/v1"})
	logmanager.StartScheduler(rootCtx, logMgrH, chAdmin)
	backupmodule.StartScheduler(rootCtx, backupSvc)

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
		// 缓存策略（M2-041 修复：无缓存头致浏览器启发式缓存旧 bundle，「改了不生效」）
		// index.html 一律 no-cache（哈希资产 URL 随内容变化，天然可长缓存）
		if strings.HasSuffix(name, "index.html") {
			c.Header("Cache-Control", "no-cache")
		} else {
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
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
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	addr := os.Getenv("IPAM_HTTP_ADDR")
	if addr == "" {
		addr = dbListenAddr()
	}
	certFile, keyFile := os.Getenv("IPAM_TLS_CERT"), os.Getenv("IPAM_TLS_KEY")
	if (certFile == "") != (keyFile == "") {
		log.Fatal("IPAM_TLS_CERT 与 IPAM_TLS_KEY 必须同时设置（只设置一个会拒绝启动，避免误用明文）")
	}
	tlsEnabled := certFile != "" && keyFile != ""
	scheme := "http"
	if tlsEnabled {
		scheme = "https"
	}
	log.Printf("control-plane %s listening on %s (%s)", version, addr, scheme)
	if !tlsEnabled {
		log.Printf("⚠️  未配置 IPAM_TLS_CERT/IPAM_TLS_KEY：当前为明文 HTTP，仅允许开发/受控内网使用")
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           newEngine(version, rootCtx),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	if tlsEnabled {
		srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	go func() {
		var err error
		if tlsEnabled {
			err = srv.ListenAndServeTLS(certFile, keyFile)
		} else {
			err = srv.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serve: %v", err)
		}
	}()
	<-rootCtx.Done()
	log.Printf("shutdown signal received, draining http")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown: %v", err)
	}
}

// dbListenAddr 从 webui_settings.server_port 读取监听端口（M2-046，UI 可编辑，重启生效）。
// IPAM_DB_DSN 未设置（内存 PoC）或读取失败时回退 :8443。
func dbListenAddr() string {
	dsn := os.Getenv("IPAM_DB_DSN")
	if dsn == "" {
		return ":8443"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return ":8443"
	}
	defer pool.Close()
	var port int
	if err := pool.QueryRow(ctx, `SELECT coalesce(server_port, 8443) FROM webui_settings WHERE id`).Scan(&port); err != nil {
		return ":8443"
	}
	if port < 1 || port > 65535 {
		return ":8443"
	}
	return ":" + strconv.Itoa(port)
}
