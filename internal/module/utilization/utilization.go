// Package utilization 子网池利用率采样与历史（M6-010B）。
// 采样来源：子网池容量 + coherence 绑定 + 预留；只统计 IPv4 池。
package utilization

import (
	"context"
	"encoding/binary"
	"net/netip"
	"sort"
	"time"

	"github.com/xiaodaoi/ipam/internal/module/ipam"
)

// Point 单次采样点。
type Point struct {
	SubnetID   string    `json:"subnetId"`
	SubnetName string    `json:"subnetName,omitempty"`
	CIDR       string    `json:"cidr,omitempty"`
	TS         time.Time `json:"ts"`
	Used       int       `json:"used"`
	Capacity   int       `json:"capacity"`
	Pct        float64   `json:"pct"`
}

// Repo 采样持久化。
type Repo interface {
	Insert(ctx context.Context, points []Point) error
	Latest(ctx context.Context) ([]Point, error)
	Series(ctx context.Context, subnetID string, since time.Time) ([]Point, error)
	Prune(ctx context.Context, before time.Time) error
}

// Sources 数据源（main 注入）。
type Sources struct {
	Subnets      func(context.Context) ([]ipam.Subnet, error)
	Reservations func(context.Context) ([]ipam.Reservation, error)
	Bindings     func(context.Context) ([]ipam.LedgerBinding, error)
}

// Service 采样与查询编排。
type Service struct {
	repo      Repo
	src       Sources
	retention time.Duration
}

func NewService(repo Repo, src Sources) *Service {
	return &Service{repo: repo, src: src, retention: 90 * 24 * time.Hour}
}

func (s *Service) Latest(ctx context.Context) ([]Point, error) { return s.repo.Latest(ctx) }
func (s *Service) Series(ctx context.Context, id string, since time.Time) ([]Point, error) {
	return s.repo.Series(ctx, id, since)
}

// Sample 计算并落库一次快照，同时清理过期数据。
func (s *Service) Sample(ctx context.Context) error {
	subs, err := s.src.Subnets(ctx)
	if err != nil {
		return err
	}
	reservations, err := s.src.Reservations(ctx)
	if err != nil {
		return err
	}
	bindings, err := s.src.Bindings(ctx)
	if err != nil {
		return err
	}
	points := Compute(subs, reservations, bindings, time.Now().UTC())
	if len(points) > 0 {
		if err := s.repo.Insert(ctx, points); err != nil {
			return err
		}
	}
	return s.repo.Prune(ctx, time.Now().UTC().Add(-s.retention))
}

// Run 周期采样（启动即采一次；失败仅记录，不阻塞）。
func (s *Service) Run(ctx context.Context, interval time.Duration, logf func(string, ...any)) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := s.Sample(ctx); err != nil && logf != nil {
			logf("[utilization] sample: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Compute 纯计算：每个 v4 子网的池容量、已用（绑定/预留落在池内）与百分比。
func Compute(subs []ipam.Subnet, reservations []ipam.Reservation, bindings []ipam.LedgerBinding, now time.Time) []Point {
	used := map[string]map[string]struct{}{}
	add := func(addr string) {
		a, err := netip.ParseAddr(addr)
		if err != nil {
			return
		}
		for i := range subs {
			if subs[i].Family != 4 || !inAnyPool(subs[i], a) {
				continue
			}
			if used[subs[i].ID] == nil {
				used[subs[i].ID] = map[string]struct{}{}
			}
			used[subs[i].ID][a.String()] = struct{}{}
			return
		}
	}
	for _, b := range bindings {
		add(b.IPv4)
	}
	for _, r := range reservations {
		add(r.IPv4)
	}

	points := make([]Point, 0, len(subs))
	for i := range subs {
		sn := &subs[i]
		if sn.Family != 4 {
			continue
		}
		capacity := 0
		for _, p := range sn.Pools {
			if n := poolSize(p.StartAddr, p.EndAddr); n > 0 {
				capacity += n
			}
		}
		if capacity <= 0 {
			continue
		}
		u := len(used[sn.ID])
		if u > capacity {
			u = capacity
		}
		points = append(points, Point{
			SubnetID: sn.ID, SubnetName: sn.Name, CIDR: sn.CIDR, TS: now,
			Used: u, Capacity: capacity, Pct: float64(u) / float64(capacity) * 100,
		})
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Pct > points[j].Pct })
	return points
}

func inAnyPool(s ipam.Subnet, a netip.Addr) bool {
	for _, p := range s.Pools {
		start, err1 := netip.ParseAddr(p.StartAddr)
		end, err2 := netip.ParseAddr(p.EndAddr)
		if err1 != nil || err2 != nil {
			continue
		}
		if a.Compare(start) >= 0 && a.Compare(end) <= 0 {
			return true
		}
	}
	return false
}

func poolSize(startS, endS string) int {
	start, err1 := netip.ParseAddr(startS)
	end, err2 := netip.ParseAddr(endS)
	if err1 != nil || err2 != nil || !start.Is4() || !end.Is4() {
		return 0
	}
	s4, e4 := start.As4(), end.As4()
	delta := uint64(binary.BigEndian.Uint32(e4[:])) - uint64(binary.BigEndian.Uint32(s4[:]))
	return int(delta) + 1
}
