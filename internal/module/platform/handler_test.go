package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/module/dashboard"
	dhcpmodule "github.com/xiaodaoi/ipam/internal/module/dhcp"
	dnsmodule "github.com/xiaodaoi/ipam/internal/module/dns"
	dualstack "github.com/xiaodaoi/ipam/internal/module/dualstack"
	"github.com/xiaodaoi/ipam/internal/module/ipam"
	logq "github.com/xiaodaoi/ipam/internal/module/logquery"
)

// logsAPI 组合用命名包装（与 cmd/control-plane/main.go 同法，避开 Handler 字段名冲突）。
type logsAPI struct{ *logq.Handler }

type dashAPI struct{ *dashboard.Handler }

type dsAPI struct{ *dualstack.Handler }

// dhcpAPI DHCP 选项与类匹配 handler 包装（platform 包内测试辅助，与 main 同构）。
type dhcpAPI struct{ *dhcpmodule.Handler }

func newTestRouter(h *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	orgStore := ipam.NewMemOrgStore()
	kea := ipam.NewNoopKea()
	subRepo := ipam.NewMemSubnetRepo()
	resRepo := ipam.NewMemReservationRepo()
	ledgerSvc := ipam.NewLedgerService(func(context.Context) ipam.LedgerSource {
		return ipam.LedgerSource{Subnets: []ipam.Subnet{}}
	}, resRepo, subRepo, nil)
	dnsSvc := dnsmodule.NewService(dnsmodule.NewMemUpstreamRepo(),
		dnsmodule.NewProber(time.Second, func(context.Context, string) (time.Duration, error) { return 0, nil }),
		fakeUnbound{})
	full := struct {
		*Handler
		*ipam.OrgHandler
		*ipam.SubnetHandler
		*ipam.LedgerHandler
		*ipam.AssetHandler
		*logsAPI
		*logq.AuditHandler
		*dashAPI
		*dsAPI
		*AuthHandler
		*UserHandler
		*dhcpAPI
		*dnsmodule.DnsHandler
		*dnsmodule.ForwardHandler
		*dnsmodule.ZoneHandler
		*dnsmodule.BlocklistHandler
		*dnsmodule.SettingsHandler
		*stubApplier
	}{h,
		ipam.NewOrgHandler(ipam.NewOrgService(orgStore)),
		ipam.NewSubnetHandler(ipam.NewSubnetService(subRepo, orgStore, kea, nil)),
		ipam.NewLedgerHandler(ledgerSvc),
		ipam.NewAssetHandler(ipam.NewAssetService(ipam.NewMemAssetRepo(), orgStore)),
		&logsAPI{logq.NewHandler(logq.NewService(logq.NewMemStore(), nil))},
		logq.NewAuditHandler(logq.NewMemAuditStore()),
		&dashAPI{dashboard.NewHandler(dashboard.NewService(logq.NewMemStore(), nil, nil, dashboard.Lights{}))},
		&dsAPI{dualstack.NewHandler(dualstack.NewMemStore())},
		NewAuthHandler(NewMemUserStore(), NewTokenBlacklist()),
		NewUserHandler(NewMemUserStore()),
		&dhcpAPI{dhcpmodule.NewHandler(dhcpmodule.NewMemStore(), nil, nil)},
		dnsmodule.NewDnsHandler(dnsSvc, nil),
		dnsmodule.NewForwardHandler(dnsmodule.NewForwardService(dnsmodule.NewMemForwardRuleRepo(), dnsmodule.NewMemUpstreamRepo(), fakeUnbound{})),
		dnsmodule.NewZoneHandler(dnsmodule.NewZoneService(dnsmodule.NewMemZoneRepo(), fakeUnbound{}), nil),
		dnsmodule.NewBlocklistHandler(dnsmodule.NewBlocklistService(dnsmodule.NewMemBlocklistRepo(), nil, fakeUnbound{}, "/tmp/rpz")),
		dnsmodule.NewSettingsHandler(dnsmodule.NewSettingsService(dnsmodule.NewMemSettingsRepo(), fakeUnbound{}, "/tmp/unbound.conf", nil), dnsmodule.NewMemSettingsRepo()),
		&stubApplier{},
	}
	apigen.RegisterHandlersWithOptions(r, full, apigen.GinServerOptions{BaseURL: "/api/v1"})
	return r
}

func TestGetSystemInfo_OK(t *testing.T) {
	r := newTestRouter(NewHandler("9.9.9-test"))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/system/info", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var info apigen.SystemInfo
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatalf("body not json: %v", err)
	}
	if info.Name != "ipam-control-plane" || info.Version != "9.9.9-test" || !info.Ready || info.GoVersion == "" {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestGetSystemInfo_DBDown_NotReady(t *testing.T) {
	h := NewHandler("9.9.9-test")
	h.SetDBProbe(func() error { return errors.New("db down") })

	w := httptest.NewRecorder()
	newTestRouter(h).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/system/info", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200（降级而非失败）", w.Code)
	}
	var info apigen.SystemInfo
	_ = json.Unmarshal(w.Body.Bytes(), &info)
	if info.Ready {
		t.Fatal("ready = true, want false when dbProbe fails")
	}
}

func TestWriteProblem_RFC9457(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/boom", func(c *gin.Context) {
		WriteProblem(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "PLATFORM_DB_DOWN", "数据库暂不可用")
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content-type = %q, want application/problem+json", ct)
	}
	var p apigen.Problem
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("bad problem body: %v", err)
	}
	if p.Code == nil || *p.Code != "PLATFORM_DB_DOWN" || p.Status != 500 || p.Type == "" {
		t.Fatalf("problem fields incomplete: %+v", p)
	}
}

// stubApplier 满足 conf/apply 接口桩。
type stubApplier struct{}

func (s *stubApplier) ApplyDnsConf(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) }

// fakeUnbound 探活/下发桩。
type fakeUnbound struct{}

func (f fakeUnbound) SyncForward(context.Context, []dnsmodule.Upstream) error { return nil }
func (f fakeUnbound) SyncForwardRules(context.Context, []dnsmodule.ForwardRule, []dnsmodule.Upstream) error {
	return nil
}
func (f fakeUnbound) AuthZoneReload(context.Context, string) error    { return nil }
func (f fakeUnbound) CheckConf(context.Context, string, string) error { return nil }
func (f fakeUnbound) Reload(context.Context) error                    { return nil }
func (f fakeUnbound) FlushZone(context.Context, string) error         { return nil }
