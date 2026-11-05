// Package systemmetric 主机与控制面指标采样与历史查询（M6-032）。
//
// 采集侧是单写者：hostmetrics.Collector 的后台协程负责采样，本包只读其快照，
// 因此 CPU 使用率始终是「固定间隔的两次读数差值」，不会因多个调用者交错而失真。
package systemmetric

import (
	"context"
	"log"
	"runtime/metrics"
	"time"

	"github.com/xiaodaoi/ipam/internal/pkg/hostmetrics"
)

// Disk 单次采样中的挂载点明细。
type Disk struct {
	Mount      string
	Device     string
	FSType     string
	TotalBytes int64
	UsedBytes  int64
	UsedPct    float64
}

// Point 一次采样。Series 返回时是「一个时间桶的聚合值」，字段语义相同。
type Point struct {
	Node string
	TS   time.Time

	CPUPct float64
	Cores  int
	Load1  float64
	Load5  float64
	Load15 float64

	MemTotalBytes int64
	MemUsedBytes  int64
	MemPct        float64
	UptimeSec     float64

	DiskRootPct    float64
	DiskMaxPct     float64
	DiskWorstMount string

	Goroutines      int
	HeapBytes       int64
	DBPoolInUse     int
	DBPoolMax       int
	LogLatestAgeSec int64 // -1 = 无数据或不可用

	Disks []Disk
}

// Summary 时间窗口内的峰值与均值。
type Summary struct {
	Node    string
	From    time.Time
	To      time.Time
	Samples int
	Since   time.Time

	CPUMax     float64
	CPUMaxAt   time.Time
	MemMax     float64
	MemMaxAt   time.Time
	DiskMax    float64
	DiskMaxAt  time.Time
	DiskMount  string
	Load1Max   float64
	Load1MaxAt time.Time

	CPUAvg  float64
	MemAvg  float64
	DiskAvg float64

	GoroutinesMax      int
	HeapMaxBytes       int64
	DBPoolInUseMax     int
	LogLatestAgeMaxSec int64
}

// Repo 持久化接口；PoC 无数据库时由 MemRepo 兜底。
type Repo interface {
	Insert(ctx context.Context, p Point) error
	Series(ctx context.Context, node string, from, to time.Time, step time.Duration) ([]Point, error)
	Summary(ctx context.Context, node string, from time.Time) (Summary, error)
	Earliest(ctx context.Context, node string) (time.Time, bool, error)
	Prune(ctx context.Context, before time.Time) error
}

// Sources 外部数据源（main 注入）。
type Sources struct {
	// Host 返回最近一次宿主快照；ok=false 表示采样器尚未就绪或首个样本不可信。
	Host func() (hostmetrics.Metrics, bool)
	// DBPool 返回连接池占用与上限；未接 PG 时返回 0,0。
	DBPool func() (inUse, max int)
	// LogLatestAge 返回最新一条日志距今秒数；不可用时 ok=false。
	LogLatestAge func(ctx context.Context) (int64, bool)
}

// Service 采样与查询编排。
type Service struct {
	repo      Repo
	src       Sources
	node      string
	interval  time.Duration
	retention time.Duration

	lastPrune time.Time
}

// DefaultInterval 默认采样间隔；DefaultRetention 默认保留 30 天。
const (
	DefaultInterval  = time.Minute
	DefaultRetention = 30 * 24 * time.Hour
	// MaxPoints 单次历史查询返回的点数上限（超出自动放大 step）。
	MaxPoints = 720
	// MaxRange 单次历史查询的时间跨度上限。
	MaxRange = 90 * 24 * time.Hour
)

func NewService(repo Repo, src Sources) *Service {
	return &Service{
		repo: repo, src: src, node: "local",
		interval: DefaultInterval, retention: DefaultRetention,
	}
}

func (s *Service) SetNode(node string) {
	if node != "" {
		s.node = node
	}
}

// SetInterval 采样间隔（<1s 视为默认值）。
func (s *Service) SetInterval(d time.Duration) {
	if d >= time.Second {
		s.interval = d
	}
}

// SetRetention 保留时长（<1h 视为默认值）。
func (s *Service) SetRetention(d time.Duration) {
	if d >= time.Hour {
		s.retention = d
	}
}

// RuntimeStats 读取控制面自身开销（goroutine 数、堆对象字节）。
// 用 runtime/metrics 而不是 runtime.ReadMemStats：前者开销更低、不 Stop-The-World。
func RuntimeStats() (goroutines int, heapBytes int64) {
	samples := []metrics.Sample{
		{Name: "/sched/goroutines:goroutines"},
		{Name: "/memory/classes/heap/objects:bytes"},
	}
	metrics.Read(samples)
	if samples[0].Value.Kind() == metrics.KindUint64 {
		goroutines = int(samples[0].Value.Uint64())
	}
	if samples[1].Value.Kind() == metrics.KindUint64 {
		heapBytes = int64(samples[1].Value.Uint64())
	}
	return goroutines, heapBytes
}

// Sample 采样一次并落库；宿主机快照不可信时跳过（不写脏样本）。
func (s *Service) Sample(ctx context.Context) error {
	hm, ok := s.src.Host()
	if !ok {
		return nil
	}
	now := time.Now().UTC().Truncate(s.interval)
	p := Point{
		Node:            s.node,
		TS:              now,
		CPUPct:          hm.CPU.UsagePct,
		Cores:           hm.CPU.Cores,
		Load1:           hm.CPU.Load1,
		Load5:           hm.CPU.Load5,
		Load15:          hm.CPU.Load15,
		MemTotalBytes:   int64(hm.Memory.TotalBytes),
		MemUsedBytes:    int64(hm.Memory.UsedBytes),
		MemPct:          hm.Memory.UsedPct,
		UptimeSec:       hm.UptimeSec,
		LogLatestAgeSec: -1,
	}
	for _, d := range hm.Disks {
		p.Disks = append(p.Disks, Disk{
			Mount: d.Mount, Device: d.Device, FSType: d.FSType,
			TotalBytes: int64(d.TotalBytes), UsedBytes: int64(d.UsedBytes), UsedPct: d.UsedPct,
		})
		if d.Mount == "/" {
			p.DiskRootPct = d.UsedPct
		}
		if d.UsedPct >= p.DiskMaxPct {
			p.DiskMaxPct = d.UsedPct
			p.DiskWorstMount = d.Mount
		}
	}
	p.Goroutines, p.HeapBytes = RuntimeStats()
	if s.src.DBPool != nil {
		p.DBPoolInUse, p.DBPoolMax = s.src.DBPool()
	}
	if s.src.LogLatestAge != nil {
		if age, ok := s.src.LogLatestAge(ctx); ok {
			p.LogLatestAgeSec = age
		}
	}

	if err := s.repo.Insert(ctx, p); err != nil {
		return err
	}
	// 清理低频执行（每小时最多一次），避免每分钟 DELETE 造成写放大。
	if now.Sub(s.lastPrune) >= time.Hour {
		s.lastPrune = now
		return s.repo.Prune(ctx, now.Add(-s.retention))
	}
	return nil
}

// Run 周期采样；失败只记日志，不影响控制面。
func (s *Service) Run(ctx context.Context, logf func(string, ...any)) {
	if logf == nil {
		logf = log.Printf
	}
	t := time.NewTicker(s.interval)
	defer t.Stop()
	// 启动后先等一个间隔：宿主采集器需要两次读数才能给出可信 CPU 值。
	select {
	case <-ctx.Done():
		return
	case <-t.C:
	}
	for {
		if err := s.Sample(ctx); err != nil {
			logf("[system-metric] sample: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// History 返回降采样后的历史序列。
func (s *Service) History(ctx context.Context, from, to time.Time, step time.Duration) ([]Point, error) {
	return s.repo.Series(ctx, s.node, from, to, step)
}

// Summary 返回窗口内峰值与均值；Since 用于提示「历史数据从何时开始」。
func (s *Service) Summary(ctx context.Context, from time.Time) (Summary, error) {
	sum, err := s.repo.Summary(ctx, s.node, from)
	if err != nil {
		return sum, err
	}
	if since, ok, e := s.repo.Earliest(ctx, s.node); e == nil && ok {
		sum.Since = since
	}
	sum.Node = s.node
	return sum, nil
}

// NormalizeRange 校验并规范化查询窗口：最大 90 天，step 最小 60s，
// 并自动放大 step 使返回点数不超过 MaxPoints。
func NormalizeRange(from, to time.Time, step time.Duration, now time.Time) (time.Time, time.Time, time.Duration, bool) {
	if to.IsZero() {
		to = now
	}
	if from.IsZero() {
		from = to.Add(-24 * time.Hour)
	}
	if step < time.Minute {
		step = time.Minute
	}
	if !from.Before(to) || to.Sub(from) > MaxRange {
		return from, to, step, false
	}
	if minStep := to.Sub(from) / MaxPoints; minStep > step {
		// 向上取整到整分钟，避免 61s 这类不整齐的桶。
		step = (minStep + time.Minute - 1) / time.Minute * time.Minute
	}
	return from, to, step, true
}
