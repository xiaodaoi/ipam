package kea

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xiaodaoi/ipam/internal/engine/lease"
)

func TestLease4Lifecycle(t *testing.T) {
	var last map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Command   string         `json:"command"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		last = req.Arguments
		switch req.Command {
		case "lease4-get-all":
			_, _ = w.Write([]byte(`[{"result":0,"arguments":{"leases":[{"ip-address":"10.0.0.5","hw-address":"02:00:00:00:00:01","subnet-id":3,"valid-lft":600}]}}]`))
		case "lease4-get":
			if req.Arguments["ip-address"] == "10.0.0.9" {
				_, _ = w.Write([]byte(`[{"result":3,"text":"not found"}]`))
				return
			}
			_, _ = w.Write([]byte(`[{"result":0,"arguments":{"ip-address":"10.0.0.5","hw-address":"02:00:00:00:00:01","subnet-id":3,"valid-lft":600}}]`))
		default:
			_, _ = w.Write([]byte(`[{"result":0,"text":"ok"}]`))
		}
	}))
	defer srv.Close()

	agent := NewCtrlAgent(srv.URL)
	ctx := context.Background()
	ls, err := agent.Lease4List(ctx)
	if err != nil || len(ls) != 1 || ls[0].SubnetID != 3 {
		t.Fatalf("list: %v %+v", err, ls)
	}
	if _, ok, err := agent.Lease4Get(ctx, "10.0.0.9"); err != nil || ok {
		t.Fatalf("get missing: ok=%v err=%v", ok, err)
	}
	l, ok, err := agent.Lease4Get(ctx, "10.0.0.5")
	if err != nil || !ok || l.HWAddress == "" {
		t.Fatalf("get: %+v %v %v", l, ok, err)
	}
	l.ValidLifetime = 1200
	l.HWAddress = "02:00:00:00:00:02"
	if err := agent.Lease4Update(ctx, lease.Lease4(l)); err != nil {
		t.Fatal(err)
	}
	if last["valid-lft"].(float64) != 1200 || last["hw-address"] != "02:00:00:00:00:02" {
		t.Fatalf("update args: %+v", last)
	}
	if err := agent.Lease4Delete(ctx, "10.0.0.5"); err != nil {
		t.Fatal(err)
	}
	if err := agent.Lease6Delete(ctx, "fd00::1", "ia_na"); err != nil {
		t.Fatal(err)
	}
	if last["type"] != "IA_NA" {
		t.Fatalf("lease6 type: %+v", last)
	}
}
