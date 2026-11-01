package logexport

import (
	"context"
	"log"
	"strconv"
	"time"
)

// WatermarkConfig 磁盘水位自动降级参数（M6-008）。
type WatermarkConfig struct {
	High        float64       // 触发降级的使用率（默认 90%）
	Low         float64       // 恢复正常的回差（默认 85%）
	Consecutive int           // 连续命中次数（默认 3，避免抖动）
	Interval    time.Duration // 采样间隔（默认 1 分钟）
}

func DefaultWatermarkConfig() WatermarkConfig {
	return WatermarkConfig{High: 90, Low: 85, Consecutive: 3, Interval: time.Minute}
}

// WatermarkConfigFromEnv 读取 IPAM_LOG_DEGRADE_HIGH/LOW，非法值回退默认。
func WatermarkConfigFromEnv(getenv func(string) string) WatermarkConfig {
	cfg := DefaultWatermarkConfig()
	if getenv == nil {
		return cfg
	}
	parse := func(key string, def float64) float64 {
		raw := getenv(key)
		if raw == "" {
			return def
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || v <= 0 || v >= 100 {
			return def
		}
		return v
	}
	cfg.High = parse("IPAM_LOG_DEGRADE_HIGH", cfg.High)
	cfg.Low = parse("IPAM_LOG_DEGRADE_LOW", cfg.Low)
	if cfg.Low >= cfg.High {
		cfg.Low = cfg.High - 5
	}
	if raw := getenv("IPAM_LOG_DEGRADE_INTERVAL"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			cfg.Interval = d
		}
	}
	return cfg
}

// Watermark 按宿主磁盘水位自动切换日志采样（M6-008）：
//   - 连续 N 次 ≥ High：覆盖采样为 errors（仅非 INFO），迅速降低入库/外发量；
//   - 连续 N 次 ≤ Low：解除覆盖，恢复用户配置；
//   - High/Low 之间保持现状（回差防抖）。
//
// 只覆盖渲染层，不修改用户保存的策略；磁盘恢复后自动回到原模式。
type Watermark struct {
	svc     *Service
	usage   func() (float64, error)
	cfg     WatermarkConfig
	highCnt int
	lowCnt  int
}

func NewWatermark(svc *Service, usage func() (float64, error), cfg WatermarkConfig) *Watermark {
	if cfg.Consecutive < 1 {
		cfg.Consecutive = 1
	}
	if cfg.Interval <= 0 {
		cfg.Interval = time.Minute
	}
	return &Watermark{svc: svc, usage: usage, cfg: cfg}
}

// Run 启动周期检查（ctx 取消即退出）。
func (w *Watermark) Run(ctx context.Context) {
	if w == nil || w.svc == nil || w.usage == nil {
		return
	}
	t := time.NewTicker(w.cfg.Interval)
	defer t.Stop()
	_, _ = w.Evaluate(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_, _ = w.Evaluate(ctx)
		}
	}
}

// Evaluate 执行一次水位判定，返回当前是否处于降级与磁盘使用率。
func (w *Watermark) Evaluate(ctx context.Context) (degraded bool, pct float64) {
	if w == nil || w.svc == nil || w.usage == nil {
		return false, 0
	}
	pct, err := w.usage()
	if err != nil {
		log.Printf("[log-export] 磁盘水位读取失败，跳过本轮自动降级: %v", err)
		return w.svc.AutoMode() != "", 0
	}
	w.svc.SetDiskPercent(pct)
	switch {
	case pct >= w.cfg.High:
		w.lowCnt = 0
		w.highCnt++
		if w.highCnt >= w.cfg.Consecutive && w.svc.AutoMode() == "" {
			if err := w.svc.SetAutoSampling(ctx, "errors"); err != nil {
				log.Printf("[log-export] 自动降级失败: %v", err)
			} else {
				log.Printf("[log-export] 磁盘使用率 %.1f%% ≥ %.0f%%，已自动切换为仅异常采样", pct, w.cfg.High)
			}
		}
	case pct <= w.cfg.Low:
		w.highCnt = 0
		w.lowCnt++
		if w.lowCnt >= w.cfg.Consecutive && w.svc.AutoMode() != "" {
			if err := w.svc.SetAutoSampling(ctx, ""); err != nil {
				log.Printf("[log-export] 解除自动降级失败: %v", err)
			} else {
				log.Printf("[log-export] 磁盘使用率 %.1f%% ≤ %.0f%%，已恢复用户采样策略", pct, w.cfg.Low)
			}
		}
	default:
		// 回差区间：保持现状，计数清零，避免在阈值附近反复切换。
		w.highCnt, w.lowCnt = 0, 0
	}
	return w.svc.AutoMode() != "", pct
}
