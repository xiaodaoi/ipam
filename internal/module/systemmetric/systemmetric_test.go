package systemmetric

import (
	"context"
	"testing"
	"time"

	"github.com/xiaodaoi/ipam/internal/pkg/hostmetrics"
)

func hostFixture(cpu, mem, root, data float64) hostmetrics.Metrics {
	return hostmetrics.Metrics{
		TS:        time.Now().UTC(),
		UptimeSec: 3600,
		CPU:       hostmetrics.CPU{UsagePct: cpu, Cores: 8, Load1: 0.5, Load5: 0.4, Load15: 0.3},
		Memory: hostmetrics.Memory{
			TotalBytes: 16 << 30, UsedBytes: 8 << 30, UsedPct: mem,
		},
		Disks: []hostmetrics.Disk{
			{Mount: "/", Device: "/dev/sda1", FSType: "ext4", TotalBytes: 100, UsedBytes: 37, UsedPct: root},
			{Mount: "/data", Device: "/dev/sdb1", FSType: "xfs", TotalBytes: 200, UsedBytes: 188, UsedPct: data},
		},
	}
}

func TestService_SampleSkipsUntilHostReady(t *testing.T) {
	repo := NewMemRepo()
	svc := NewService(repo, Sources{Host: func() (hostmetrics.Metrics, bool) {
		return hostmetrics.Metrics{}, false // 首个样本无 CPU 差值
	}})
	if err := svc.Sample(context.Background()); err != nil {
		t.Fatal(err)
	}
	points, _ := repo.Series(context.Background(), "local", time.Time{}, time.Now().Add(time.Hour), time.Minute)
	if len(points) != 0 {
		t.Fatalf("宿主未就绪时不应落库，got %d 条", len(points))
	}
}

func TestService_SampleComputesDiskExtremes(t *testing.T) {
	repo := NewMemRepo()
	svc := NewService(repo, Sources{Host: func() (hostmetrics.Metrics, bool) {
		return hostFixture(12.5, 50, 37.1, 94), true
	}})
	svc.SetRetention(time.Hour)
	if err := svc.Sample(context.Background()); err != nil {
		t.Fatal(err)
	}
	sum, err := svc.Summary(context.Background(), time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if sum.Samples != 1 {
		t.Fatalf("samples = %d, want 1", sum.Samples)
	}
	if sum.DiskMax != 94 || sum.DiskMount != "/data" {
		t.Fatalf("disk max = %v @%q, want 94 @/data", sum.DiskMax, sum.DiskMount)
	}
	if sum.CPUMax != 12.5 {
		t.Fatalf("cpu max = %v, want 12.5", sum.CPUMax)
	}
	if sum.GoroutinesMax <= 0 || sum.HeapMaxBytes <= 0 {
		t.Fatalf("控制面指标未采集: goroutines=%d heap=%d", sum.GoroutinesMax, sum.HeapMaxBytes)
	}
	if sum.LogLatestAgeMaxSec != -1 {
		t.Fatalf("未接日志源时应为 -1，got %d", sum.LogLatestAgeMaxSec)
	}
}

func TestService_SampleHasNoDisks(t *testing.T) {
	repo := NewMemRepo()
	svc := NewService(repo, Sources{Host: func() (hostmetrics.Metrics, bool) {
		m := hostFixture(5, 10, 0, 0)
		m.Disks = nil
		return m, true
	}})
	if err := svc.Sample(context.Background()); err != nil {
		t.Fatal(err)
	}
	sum, _ := svc.Summary(context.Background(), time.Now().Add(-time.Hour))
	if sum.Samples != 1 || sum.DiskMax != 0 || sum.DiskMount != "" {
		t.Fatalf("无挂载点时应写 0/空，got samples=%d max=%v mount=%q", sum.Samples, sum.DiskMax, sum.DiskMount)
	}
}

func TestService_LogAgeRecorded(t *testing.T) {
	repo := NewMemRepo()
	svc := NewService(repo, Sources{
		Host:         func() (hostmetrics.Metrics, bool) { return hostFixture(1, 2, 3, 4), true },
		LogLatestAge: func(context.Context) (int64, bool) { return 187, true },
	})
	if err := svc.Sample(context.Background()); err != nil {
		t.Fatal(err)
	}
	sum, _ := svc.Summary(context.Background(), time.Now().Add(-time.Hour))
	if sum.LogLatestAgeMaxSec != 187 {
		t.Fatalf("log age = %d, want 187", sum.LogLatestAgeMaxSec)
	}
}

func TestMemRepo_DuplicateTSIgnored(t *testing.T) {
	repo := NewMemRepo()
	ts := time.Now().UTC().Truncate(time.Minute)
	if err := repo.Insert(context.Background(), Point{Node: "local", TS: ts, CPUPct: 10}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Insert(context.Background(), Point{Node: "local", TS: ts, CPUPct: 90}); err != nil {
		t.Fatal(err)
	}
	points, _ := repo.Series(context.Background(), "local", ts.Add(-time.Minute), ts.Add(time.Minute), time.Minute)
	if len(points) != 1 || points[0].CPUPct != 10 {
		t.Fatalf("重复 (node,ts) 应保留首条，got %+v", points)
	}
}

func TestNormalizeRange(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

	from, to, step, ok := NormalizeRange(time.Time{}, time.Time{}, 0, now)
	if !ok {
		t.Fatal("缺省窗口应合法")
	}
	if !to.Equal(now) || !from.Equal(now.Add(-24*time.Hour)) {
		t.Fatalf("缺省窗口错误: %v ~ %v", from, to)
	}
	if step < time.Minute {
		t.Fatalf("step 应至少 60s，got %v", step)
	}
	if pts := int(to.Sub(from)/step) + 1; pts > MaxPoints+1 {
		t.Fatalf("点数 %d 超过上限 %d（step=%v）", pts, MaxPoints, step)
	}

	if _, _, _, ok := NormalizeRange(now.Add(-91*24*time.Hour), now, 0, now); ok {
		t.Fatal("超过 90 天应判为非法")
	}
	if _, _, _, ok := NormalizeRange(now, now.Add(-time.Hour), 0, now); ok {
		t.Fatal("from >= to 应判为非法")
	}
	if _, _, step, ok := NormalizeRange(now.Add(-time.Hour), now, 10*time.Second, now); !ok || step != time.Minute {
		t.Fatalf("step 应被抬到 60s，got %v", step)
	}
	// 1 小时窗口不需要放大：60s → 60 点。
	if _, _, step, _ := NormalizeRange(now.Add(-time.Hour), now, time.Minute, now); step != time.Minute {
		t.Fatalf("1 小时窗口 step 不应被放大，got %v", step)
	}
	// 30 天窗口：3600s → 720 点。
	from30, to30, step30, ok := NormalizeRange(now.Add(-30*24*time.Hour), now, time.Minute, now)
	if !ok {
		t.Fatal("30 天窗口应合法")
	}
	if pts := int(to30.Sub(from30)/step30) + 1; pts > MaxPoints+1 {
		t.Fatalf("30 天点数 %d 超过上限（step=%v）", pts, step30)
	}
}

func TestSeriesBucketAggregation(t *testing.T) {
	repo := NewMemRepo()
	base := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		_ = repo.Insert(context.Background(), Point{
			Node: "local", TS: base.Add(time.Duration(i) * 30 * time.Second),
			CPUPct: float64(10 + i*10), MemPct: 20, DiskMaxPct: float64(i),
		})
	}
	points, err := repo.Series(context.Background(), "local", base, base.Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 {
		t.Fatalf("应为 2 个桶，got %d", len(points))
	}
	if points[0].CPUPct != 15 || points[1].CPUPct != 35 {
		t.Fatalf("桶内均值错误: %v / %v", points[0].CPUPct, points[1].CPUPct)
	}
	if points[1].DiskMaxPct != 3 {
		t.Fatalf("桶内磁盘应取最大值 3，got %v", points[1].DiskMaxPct)
	}
}
