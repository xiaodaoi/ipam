package dhcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/engine/lease"
)

type fakeLeaseAgent struct {
	leases  []lease.Lease4
	updated *lease.Lease4
	deleted []string
}

func (f *fakeLeaseAgent) Lease4List(context.Context) ([]lease.Lease4, error) { return f.leases, nil }
func (f *fakeLeaseAgent) Lease4Get(_ context.Context, ip string) (lease.Lease4, bool, error) {
	for _, l := range f.leases {
		if l.IPAddress == ip {
			return l, true, nil
		}
	}
	return lease.Lease4{}, false, nil
}
func (f *fakeLeaseAgent) Lease4Update(_ context.Context, l lease.Lease4) error {
	f.updated = &l
	return nil
}
func (f *fakeLeaseAgent) Lease4Delete(_ context.Context, ip string) error {
	f.deleted = append(f.deleted, ip)
	return nil
}
func (f *fakeLeaseAgent) Lease6Delete(_ context.Context, ip, _ string) error {
	f.deleted = append(f.deleted, ip)
	return nil
}

func leaseCtx(method, body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, "/", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, w
}

func TestListDhcpLeases4Sorted(t *testing.T) {
	h := NewLeaseHandler(&fakeLeaseAgent{leases: []lease.Lease4{
		{IPAddress: "10.0.0.20", SubnetID: 2}, {IPAddress: "10.0.0.3", SubnetID: 1},
	}})
	c, w := leaseCtx(http.MethodGet, "")
	h.ListDhcpLeases4(c)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d", w.Code)
	}
	var got apigen.DhcpLease4List
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if len(got.Items) != 2 || got.Items[0].IpAddress != "10.0.0.3" {
		t.Fatalf("items=%+v", got.Items)
	}
}

func TestUpdateDhcpLease4(t *testing.T) {
	fake := &fakeLeaseAgent{leases: []lease.Lease4{{IPAddress: "10.0.0.5", SubnetID: 2, HWAddress: "02:00:00:00:00:01", ValidLifetime: 600}}}
	h := NewLeaseHandler(fake)

	c, w := leaseCtx(http.MethodPatch, `{"hwAddress":"bad"}`)
	h.UpdateDhcpLease4(c, "10.0.0.5")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid mac code=%d", w.Code)
	}

	c, w = leaseCtx(http.MethodPatch, `{"validLifetime":1200}`)
	h.UpdateDhcpLease4(c, "10.0.0.9")
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing code=%d", w.Code)
	}

	c, w = leaseCtx(http.MethodPatch, `{"validLifetime":1200,"hwAddress":"02:00:00:00:00:02","hostname":"host-a"}`)
	h.UpdateDhcpLease4(c, "10.0.0.5")
	if w.Code != http.StatusOK || fake.updated == nil {
		t.Fatalf("code=%d updated=%+v", w.Code, fake.updated)
	}
	if fake.updated.ValidLifetime != 1200 || fake.updated.HWAddress != "02:00:00:00:00:02" || fake.updated.Hostname != "host-a" {
		t.Fatalf("updated=%+v", fake.updated)
	}
}

func TestDeleteLeases(t *testing.T) {
	fake := &fakeLeaseAgent{}
	h := NewLeaseHandler(fake)
	c, _ := leaseCtx(http.MethodDelete, "")
	h.DeleteDhcpLease4(c, "10.0.0.5")
	if c.Writer.Status() != http.StatusNoContent || len(fake.deleted) != 1 {
		t.Fatalf("v4 code=%d deleted=%v", c.Writer.Status(), fake.deleted)
	}
	c, _ = leaseCtx(http.MethodDelete, "")
	h.DeleteDhcpLease6(c, "fd00::1", apigen.DeleteDhcpLease6Params{LeaseType: apigen.DeleteDhcpLease6ParamsLeaseTypeIANA})
	if c.Writer.Status() != http.StatusNoContent || len(fake.deleted) != 2 {
		t.Fatalf("v6 code=%d deleted=%v", c.Writer.Status(), fake.deleted)
	}
}
