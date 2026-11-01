package platform

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// SecurityHeaders 基础安全响应头（M6-001 / M6-017 补全）。
// csp 为空时回退到环境变量 IPAM_SECURITY_CSP；两者皆空则不注入 CSP。
// forceHSTS：TLS 终结在反代之后时（X-Forwarded-Proto: https）也下发 HSTS。
func SecurityHeaders(csp ...string) gin.HandlerFunc {
	policy := ""
	if len(csp) > 0 {
		policy = csp[0]
	}
	if policy == "" {
		policy = os.Getenv("IPAM_SECURITY_CSP")
	}
	forceHSTS := envTruthy(os.Getenv("IPAM_FORCE_HSTS"))
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("X-Permitted-Cross-Domain-Policies", "none")
		h.Set("X-DNS-Prefetch-Control", "off")
		// 旧版 IE 的 XSS 过滤器会引入新问题，显式关闭（现代浏览器应依赖 CSP）。
		h.Set("X-XSS-Protection", "0")
		if c.Request.TLS != nil || (forceHSTS && strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")) {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		if policy != "" {
			h.Set("Content-Security-Policy", policy)
		}
		c.Next()
	}
}

// BodyLimit 限制 /api/v1/* 请求体大小（M6-017，防内存 DoS）。
// maxBytes<=0 关闭。超限由 net/http 的 MaxBytesReader 触发 413。
func BodyLimit(maxBytes int64) gin.HandlerFunc {
	if maxBytes <= 0 {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		if !strings.HasPrefix(c.Request.URL.Path, "/api/v1") {
			c.Next()
			return
		}
		// 已知长度时前置拒绝，返回确定性 413（MaxBytesReader 在 handler 已写响应后不会覆写成 413）。
		if c.Request.ContentLength > maxBytes {
			c.Header("Connection", "close")
			WriteProblem(c, http.StatusRequestEntityTooLarge,
				"https://ipam.local/problems/payload-too-large", "PAYLOAD_TOO_LARGE",
				"请求体超过允许大小（"+strconv.FormatInt(maxBytes, 10)+" 字节）")
			c.Abort()
			return
		}
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}

// RejectTrace 拒绝 TRACE/TRACK（跨站追踪/XST 类风险），返回 405。
func RejectTrace() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodTrace || c.Request.Method == "TRACK" {
			WriteProblem(c, http.StatusMethodNotAllowed,
				"https://ipam.local/problems/method-not-allowed", "METHOD_NOT_ALLOWED", "不支持的 HTTP 方法")
			c.Abort()
			return
		}
		c.Next()
	}
}

// MetricsAuth 对 /metrics 做可选 Bearer 令牌保护（M6-017）。
// token 为空时放行（依赖内网/Compose 内部网络隔离），并由启动日志提示风险。
func MetricsAuth(token string) gin.HandlerFunc {
	if token == "" {
		return func(c *gin.Context) { c.Next() }
	}
	want := []byte("Bearer " + token)
	return func(c *gin.Context) {
		got := []byte(c.GetHeader("Authorization"))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			WriteProblem(c, http.StatusUnauthorized,
				"https://ipam.local/problems/unauthorized", "UNAUTHORIZED", "指标端点需要 Bearer 令牌")
			c.Abort()
			return
		}
		c.Next()
	}
}

// DefaultHTMLCSP 依据 index.html 内联脚本生成 CSP（M6-017）。
// 脚本以 sha256 哈希放行，避免 'unsafe-inline'；样式沿用 'unsafe-inline'（Vue 运行期注入内联样式）。
// 环境变量 IPAM_SECURITY_CSP 非空时优先；IPAM_SECURITY_CSP_OFF=1 时返回空串（完全关闭）。
func DefaultHTMLCSP(indexHTML []byte) string {
	if v := os.Getenv("IPAM_SECURITY_CSP"); v != "" {
		return v
	}
	if envTruthy(os.Getenv("IPAM_SECURITY_CSP_OFF")) {
		return ""
	}
	script := "'self'"
	for _, h := range inlineScriptHashes(indexHTML) {
		script += " '" + h + "'"
	}
	return strings.Join([]string{
		"default-src 'self'",
		"script-src " + script,
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: blob: https://api.iconify.design",
		"font-src 'self' data:",
		"connect-src 'self' https://api.iconify.design https://api.simplesvg.com https://api.unisvg.com",
		"worker-src 'self' blob:",
		"frame-ancestors 'none'",
		"base-uri 'self'",
		"form-action 'self'",
		"object-src 'none'",
	}, "; ")
}

var inlineScriptRe = regexp.MustCompile(`(?is)<script([^>]*)>(.*?)</script>`)

// inlineScriptHashes 返回 HTML 中所有内联 <script>（无 src 属性）内容的 sha256 CSP 哈希。
func inlineScriptHashes(html []byte) []string {
	var out []string
	for _, m := range inlineScriptRe.FindAllSubmatch(html, -1) {
		attrs := strings.ToLower(string(m[1]))
		if strings.Contains(attrs, "src=") {
			continue
		}
		sum := sha256.Sum256(m[2])
		out = append(out, "sha256-"+base64.StdEncoding.EncodeToString(sum[:]))
	}
	return out
}

// envTruthy 解析布尔型环境变量（1/true/on/yes）。
func envTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
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
