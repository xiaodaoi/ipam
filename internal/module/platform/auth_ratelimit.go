package platform

import (
	"sync"
	"time"
)

// loginRateLimiter 登录限流（M5-032）：每客户端 IP 固定窗口计数（5 次/分钟）。
// 无后台 goroutine：窗口过期桶在访问时惰性周期清扫（距上次清扫满一个窗口即全表扫描），
// 规避长运行进程的 goroutine 生命周期问题；窗口过后计数自动复位，合法冷却重试不受限。
type loginRateLimiter struct {
	mu        sync.Mutex
	buckets   map[string]loginBucket
	limit     int
	window    time.Duration
	now       func() time.Time
	lastSweep time.Time
}

type loginBucket struct {
	windowStart time.Time
	count       int
}

func newLoginRateLimiter(now func() time.Time) *loginRateLimiter {
	return &loginRateLimiter{
		buckets: map[string]loginBucket{},
		limit:   5,
		window:  time.Minute,
		now:     now,
	}
}

// allow 计数一次尝试并判定是否放行；限流时 retryAfter 为窗口剩余时长（供 Retry-After 头）。
func (l *loginRateLimiter) allow(ip string) (allowed bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Sub(l.lastSweep) >= l.window {
		l.lastSweep = now
		for k, b := range l.buckets {
			if now.Sub(b.windowStart) >= l.window {
				delete(l.buckets, k)
			}
		}
	}
	b := l.buckets[ip]
	if now.Sub(b.windowStart) >= l.window {
		b = loginBucket{windowStart: now}
	}
	b.count++
	l.buckets[ip] = b
	if b.count > l.limit {
		return false, l.window - now.Sub(b.windowStart)
	}
	return true, 0
}
