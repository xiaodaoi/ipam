package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// 写接口加固（M6-021）：
//  1. Idempotency-Key 幂等重放——同一 key + 同一请求指纹重放首次响应，
//     key 复用于不同载荷返回 422；并发同 key 返回 409。
//  2. WriteRateLimit 对非安全方法（POST/PUT/PATCH/DELETE）单独限流，
//     避免登录以外的写接口成为暴力枚举/资源耗尽入口。

const (
	// IdempotencyHeader 幂等键请求头。
	IdempotencyHeader = "Idempotency-Key"
	// IdempotentReplayHeader 重放标记，便于客户端/排障识别。
	IdempotentReplayHeader = "Idempotent-Replay"

	defaultIdempotencyEntries = 4096
	maxIdempotencyKeyLen      = 128
	minIdempotencyKeyLen      = 8
	// maxReplayBody 单条响应快照上限：超过则放弃缓存（接口应避免超大同步响应）。
	maxReplayBody = 1 << 20
)

// Idempotency 幂等中间件。ttl<=0 时使用默认 24h。
func Idempotency(ttl time.Duration) gin.HandlerFunc {
	st := newIdempotencyStore(ttl)
	return st.middleware
}

type idempotencyEntry struct {
	fingerprint string
	status      int
	contentType string
	body        []byte
	at          time.Time
	inFlight    bool
}

type idempotencyStore struct {
	mu      sync.Mutex
	items   map[string]*idempotencyEntry
	ttl     time.Duration
	max     int
	now     func() time.Time
	sweptAt time.Time
}

func newIdempotencyStore(ttl time.Duration) *idempotencyStore {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &idempotencyStore{
		items: map[string]*idempotencyEntry{},
		ttl:   ttl,
		max:   defaultIdempotencyEntries,
		now:   time.Now,
	}
}

func (s *idempotencyStore) middleware(c *gin.Context) {
	if !isMutating(c.Request.Method) || !strings.HasPrefix(c.Request.URL.Path, "/api/v1") {
		c.Next()
		return
	}
	key := strings.TrimSpace(c.GetHeader(IdempotencyHeader))
	if key == "" {
		c.Next()
		return
	}
	if len(key) < minIdempotencyKeyLen || len(key) > maxIdempotencyKeyLen || !validIdempotencyKey(key) {
		WriteProblem(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request",
			"IDEMPOTENCY_KEY_INVALID",
			"Idempotency-Key 需为 8-128 位字母/数字/._:- 组成的字符串")
		c.Abort()
		return
	}

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxReplayBody+1))
	if err != nil {
		WriteProblem(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request",
			"BODY_READ_FAILED", "请求体读取失败")
		c.Abort()
		return
	}
	_ = c.Request.Body.Close()
	// 还原请求体供后续 handler 读取。
	c.Request.Body = io.NopCloser(bytes.NewReader(body))

	fp := fingerprint(c, body)
	scope := key + "|" + c.Request.Method + " " + c.Request.URL.Path

	created, existing := s.begin(scope, fp)
	if existing != nil {
		if existing.fingerprint != fp {
			WriteProblem(c, http.StatusUnprocessableEntity,
				"https://ipam.local/problems/idempotency-conflict",
				"IDEMPOTENCY_KEY_REUSED", "该 Idempotency-Key 已用于不同请求")
			c.Abort()
			return
		}
		c.Header(IdempotentReplayHeader, "true")
		if existing.contentType != "" {
			c.Header("Content-Type", existing.contentType)
		}
		c.Status(existing.status)
		_, _ = c.Writer.Write(existing.body)
		c.Abort()
		return
	}
	if created == nil {
		WriteProblem(c, http.StatusConflict, "https://ipam.local/problems/idempotency-in-progress",
			"IDEMPOTENCY_IN_PROGRESS", "相同 Idempotency-Key 的请求正在处理中，请稍后重试")
		c.Abort()
		return
	}

	// 首次执行：录制响应后落库。
	rec := &recordingWriter{ResponseWriter: c.Writer}
	c.Writer = rec
	c.Next()

	s.finish(scope, fp, rec)
}

// begin 返回 (占位条目, 已完成条目)。占位条目非 nil 表示需要执行。
// 返回 (nil, nil) 表示同 key 正在执行。
func (s *idempotencyStore) begin(scope, fp string) (*idempotencyEntry, *idempotencyEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.sweepLocked(now)
	if e, ok := s.items[scope]; ok {
		if e.inFlight {
			return nil, nil
		}
		if now.Sub(e.at) <= s.ttl {
			return nil, e
		}
		delete(s.items, scope)
	}
	e := &idempotencyEntry{fingerprint: fp, inFlight: true, at: now}
	s.items[scope] = e
	s.evictLocked()
	return e, nil
}

func (s *idempotencyStore) finish(scope, fp string, rec *recordingWriter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.items[scope]
	if !ok || e.fingerprint != fp {
		return
	}
	// 5xx 不缓存：让客户端可以重试（服务端错误通常非幂等可重放）。
	if rec.status >= 500 || rec.overflow || len(rec.buf.Bytes()) > maxReplayBody {
		delete(s.items, scope)
		return
	}
	e.inFlight = false
	e.status = rec.status
	e.contentType = rec.Header().Get("Content-Type")
	e.body = append([]byte(nil), rec.buf.Bytes()...)
	e.at = s.now()
}

func (s *idempotencyStore) sweepLocked(now time.Time) {
	if now.Sub(s.sweptAt) < time.Minute {
		return
	}
	s.sweptAt = now
	for k, e := range s.items {
		if !e.inFlight && now.Sub(e.at) > s.ttl {
			delete(s.items, k)
		}
	}
}

func (s *idempotencyStore) evictLocked() {
	if len(s.items) <= s.max {
		return
	}
	// 简单淘汰：优先删已完成的最旧条目（容量上限保护，非 LRU）。
	var oldestKey string
	var oldest time.Time
	for k, e := range s.items {
		if e.inFlight {
			continue
		}
		if oldestKey == "" || e.at.Before(oldest) {
			oldestKey, oldest = k, e.at
		}
	}
	if oldestKey != "" {
		delete(s.items, oldestKey)
	}
}

func fingerprint(c *gin.Context, body []byte) string {
	h := sha256.New()
	_, _ = h.Write([]byte(c.Request.Method))
	_, _ = h.Write([]byte(" "))
	_, _ = h.Write([]byte(c.Request.URL.Path))
	_, _ = h.Write([]byte("?"))
	_, _ = h.Write([]byte(c.Request.URL.RawQuery))
	_, _ = h.Write([]byte("\n"))
	_, _ = h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

func validIdempotencyKey(k string) bool {
	for _, r := range k {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '_', r == ':', r == '-':
		default:
			return false
		}
	}
	return true
}

func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// recordingWriter 录制响应体（供幂等重放），同时保持流式写语义。
type recordingWriter struct {
	gin.ResponseWriter
	buf      bytes.Buffer
	status   int
	overflow bool
}

func (w *recordingWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *recordingWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if !w.overflow {
		if w.buf.Len()+len(b) > maxReplayBody {
			w.overflow = true
			w.buf.Reset()
		} else {
			w.buf.Write(b)
		}
	}
	return w.ResponseWriter.Write(b)
}

func (w *recordingWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

// WriteRateLimit 写接口限流（M6-021）：仅对非安全方法计数。
// perMinute<=0 关闭。与全局 APIRateLimit 叠加（全局仍覆盖读请求）。
func WriteRateLimit(perMinute int) gin.HandlerFunc {
	if perMinute <= 0 {
		return func(c *gin.Context) { c.Next() }
	}
	l := newFixedWindowLimiter(perMinute, time.Minute)
	return func(c *gin.Context) {
		if !strings.HasPrefix(c.Request.URL.Path, "/api/v1") || !isMutating(c.Request.Method) {
			c.Next()
			return
		}
		allowed, retryAfter := l.allow(c.ClientIP())
		if !allowed {
			c.Header("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
			WriteProblem(c, http.StatusTooManyRequests, "https://ipam.local/problems/rate-limit",
				"WRITE_RATE_LIMITED", "写入请求过于频繁，请稍后重试")
			c.Abort()
			return
		}
		c.Next()
	}
}

