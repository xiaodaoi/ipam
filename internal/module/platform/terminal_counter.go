package platform

import (
	"context"
	"errors"
	"sync"
	"time"
)

// TerminalCounter 活跃终端计数的 TTL 缓存（M6-004B）。
//
// 计数源通常是一次 ClickHouse 近 24h 去重 MAC 扫描：/system/license 与 /metrics
// 都会取用，必须缓存，否则每次抓取都扫日志表。数据源失败时返回上次成功值（降级），
// 从未成功过才返回错误。
type TerminalCounter struct {
	fn  func(context.Context) (int64, error)
	ttl time.Duration
	now func() time.Time

	mu  sync.Mutex
	val int64
	at  time.Time
	ok  bool
}

func NewTerminalCounter(fn func(context.Context) (int64, error), ttl time.Duration) *TerminalCounter {
	if ttl <= 0 {
		ttl = time.Minute
	}
	return &TerminalCounter{fn: fn, ttl: ttl, now: time.Now}
}

// Count 返回活跃终端数（ttl 内复用缓存）。
func (c *TerminalCounter) Count(ctx context.Context) (int64, error) {
	if c == nil || c.fn == nil {
		return 0, errors.New("未配置活跃终端计数源")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if c.ok && now.Sub(c.at) < c.ttl {
		return c.val, nil
	}
	v, err := c.fn(ctx)
	if err != nil {
		if c.ok {
			return c.val, nil
		}
		return 0, err
	}
	c.val, c.at, c.ok = v, now, true
	return v, nil
}
