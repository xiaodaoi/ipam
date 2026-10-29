package platform

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// SecurityHeaders 基础安全响应头（M6-001）。
// CSP 默认不注入（Vben 生产包使用内联样式，盲目开启会破坏页面）；
// 需要更严格策略时通过 IPAM_SECURITY_CSP 显式配置。
func SecurityHeaders() gin.HandlerFunc {
	csp := os.Getenv("IPAM_SECURITY_CSP")
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		if c.Request.TLS != nil {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		if csp != "" {
			h.Set("Content-Security-Policy", csp)
		}
		c.Next()
	}
}

// APIRateLimit 通用 API 限流（M6-001）：固定窗口、按客户端 IP 计数。
// 默认 600 次/分钟；IPAM_RATE_LIMIT=0 关闭。仅作用于 /api/v1/*，避免静态资源误伤。
func APIRateLimit(perMinute int) gin.HandlerFunc {
	if perMinute <= 0 {
		return func(c *gin.Context) { c.Next() }
	}
	l := newFixedWindowLimiter(perMinute, time.Minute)
	return func(c *gin.Context) {
		if !strings.HasPrefix(c.Request.URL.Path, "/api/v1") {
			c.Next()
			return
		}
		allowed, retryAfter := l.allow(c.ClientIP())
		if !allowed {
			c.Header("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
			WriteProblem(c, 429, "https://ipam.local/problems/rate-limit",
				"RATE_LIMITED", "请求过于频繁，请稍后重试")
			c.Abort()
			return
		}
		c.Next()
	}
}

// fixedWindowLimiter 无后台 goroutine 的固定窗口计数器（惰性清扫过期桶）。
type fixedWindowLimiter struct {
	mu        sync.Mutex
	buckets   map[string]rateBucket
	limit     int
	window    time.Duration
	now       func() time.Time
	lastSweep time.Time
}

type rateBucket struct {
	windowStart time.Time
	count       int
}

func newFixedWindowLimiter(limit int, window time.Duration) *fixedWindowLimiter {
	return &fixedWindowLimiter{
		buckets: map[string]rateBucket{}, limit: limit, window: window,
		now: time.Now, lastSweep: time.Now(),
	}
}

func (l *fixedWindowLimiter) allow(ip string) (bool, time.Duration) {
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
	if now.Sub(b.windowStart) >= l.window || b.windowStart.IsZero() {
		b = rateBucket{windowStart: now}
	}
	b.count++
	l.buckets[ip] = b
	if b.count > l.limit {
		return false, l.window - now.Sub(b.windowStart)
	}
	return true, 0
}
