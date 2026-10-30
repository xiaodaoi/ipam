package utilization

import (
	"context"
	"testing"
	"time"

	"github.com/xiaodaoi/ipam/internal/module/ipam"
)

func TestCompute(t *testing.T) {
	now := time.Date(2026, 10, 30, 3, 0, 0, 0, time.UTC)
	subs := []ipam.Subnet{
		{ID: "s1", Name: "办公", Family: 4, CIDR: "10.0.0.0/24", Pools: []ipam.Pool{{StartAddr: "10.0.0.10", EndAddr: "10.0.0.19"}}},
		{ID: "s2", Name: "IPv6", Family: 6, CIDR: "fd00::/64"},
	}
	res := []ipam.Reservation{{IPv4: "10.0.0.10"}}
	binds := []ipam.LedgerBinding{
		{IPv4: "10.0.0.11"}, {IPv4: "10.0.0.11"}, // 去重
		{IPv4: "10.0.0.12"}, {IPv4: "10.0.0.99"}, // 池外不计
	}
	points := Compute(subs, res, binds, now)
	if len(points) != 1 {
		t.Fatalf("points=%+v", points)
	}
	p := points[0]
	if p.Capacity != 10 || p.Used != 3 || p.Pct != 30 || p.SubnetName != "办公" {
		t.Fatalf("point=%+v", p)
	}
}

func TestMemRepoLatestSeriesPrune(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	now := time.Now().UTC()
	_ = repo.Insert(ctx, []Point{
		{SubnetID: "s1", TS: now.Add(-2 * time.Hour), Used: 1, Capacity: 10, Pct: 10},
		{SubnetID: "s1", TS: now.Add(-time.Hour), Used: 3, Capacity: 10, Pct: 30},
		{SubnetID: "s2", TS: now.Add(-time.Hour), Used: 5, Capacity: 10, Pct: 50},
		{SubnetID: "s3", TS: now.Add(-100 * 24 * time.Hour), Used: 1, Capacity: 1, Pct: 100},
	})
	latest, _ := repo.Latest(ctx)
	if len(latest) != 3 {
		t.Fatalf("latest=%d", len(latest))
	}
	for _, p := range latest {
		if p.SubnetID == "s1" && p.Used != 3 {
			t.Fatalf("s1 latest=%+v", p)
		}
	}
	series, _ := repo.Series(ctx, "s1", now.Add(-3*time.Hour))
	if len(series) != 2 {
		t.Fatalf("series=%d", len(series))
	}
	_ = repo.Prune(ctx, now.Add(-90*24*time.Hour))
	latest, _ = repo.Latest(ctx)
	if len(latest) != 2 {
		t.Fatalf("after prune=%d", len(latest))
	}
}
