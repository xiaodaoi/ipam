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
	dnsmodule "github.com/xiaodaoi/ipam/internal/module/dns"
	"github.com/xiaodaoi/ipam/internal/module/ipam"
)

func newTestRouter(h *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	orgStore := ipam.NewMemOrgStore()
	kea := ipam.NewNoopKea()
	subRepo := ipam.NewMemSubnetRepo()
	resRepo := ipam.NewMemReservationRepo()
	ledgerSvc := ipam.NewLedgerService(func(context.Context) ipam.LedgerSource {
		return ipam.LedgerSource{Subnets: []ipam.Subnet{}}
	}, resRepo, kea, subRepo)
	dnsSvc := dnsmodule.NewService(dnsmodule.NewMemUpstreamRepo(),
		dnsmodule.NewProber(time.Second, func(context.Context, string) (time.Duration, error) { return 0, nil }),
		fakeUnbound{})
	full := struct {
		*Handler
		*ipam.OrgHandler
		*ipam.SubnetHandler
		*ipam.LedgerHandler
		*ipam.AssetHandler
		*dnsmodule.DnsHandler
		*dnsmodule.ForwardHandler
	}{h,
		ipam.NewOrgHandler(ipam.NewOrgService(orgStore)),
		ipam.NewSubnetHandler(ipam.NewSubnetService(subRepo, orgStore, kea)),
		ipam.NewLedgerHandler(ledgerSvc),
		ipam.NewAssetHandler(ipam.NewAssetService(ipam.NewMemAssetRepo(), orgStore)),
		dnsmodule.NewDnsHandler(dnsSvc),
		dnsmodule.NewForwardHandler(dnsmodule.NewForwardService(dnsmodule.NewMemForwardRuleRepo(), dnsmodule.NewMemUpstreamRepo(), fakeUnbound{})),
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

// fakeUnbound 探活/下发桩。
type fakeUnbound struct{}

func (f fakeUnbound) SyncForward(context.Context, []dnsmodule.Upstream) error { return nil }
func (f fakeUnbound) SyncForwardRules(context.Context, []dnsmodule.ForwardRule, []dnsmodule.Upstream) error {
	return nil
}
