package platform

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func idemTestRouter(t *testing.T) (*gin.Engine, *int32) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var calls int32
	r := gin.New()
	r.Use(Idempotency(time.Hour))
	r.POST("/api/v1/things", func(c *gin.Context) {
		n := atomic.AddInt32(&calls, 1)
		c.JSON(http.StatusCreated, gin.H{"calls": n})
	})
	return r, &calls
}

func doPost(r *gin.Engine, key, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/things", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set(IdempotencyHeader, key)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestIdempotencyReplay(t *testing.T) {
	r, calls := idemTestRouter(t)

	first := doPost(r, "key-abcdefgh", `{"a":1}`)
	if first.Code != http.StatusCreated {
		t.Fatalf("首次请求状态=%d body=%s", first.Code, first.Body.String())
	}
	second := doPost(r, "key-abcdefgh", `{"a":1}`)
	if second.Code != http.StatusCreated || second.Body.String() != first.Body.String() {
		t.Fatalf("重放响应不一致: %d %s vs %s", second.Code, second.Body.String(), first.Body.String())
	}
	if second.Header().Get(IdempotentReplayHeader) != "true" {
		t.Fatalf("缺少重放标记: %v", second.Header())
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("handler 执行次数=%d want 1", got)
	}
}

func TestIdempotencyKeyReuseWithDifferentBody(t *testing.T) {
	r, calls := idemTestRouter(t)
	first := doPost(r, "key-abcdefgh", `{"a":1}`)
	if first.Code != http.StatusCreated {
		t.Fatalf("首次=%d", first.Code)
	}
	second := doPost(r, "key-abcdefgh", `{"a":2}`)
	if second.Code != http.StatusUnprocessableEntity {
		t.Fatalf("不同载荷复用 key 应 422，实际=%d body=%s", second.Code, second.Body.String())
	}
	var p WriteJSONProblemLite
	_ = json.Unmarshal(second.Body.Bytes(), &p)
	if p.Code != "IDEMPOTENCY_KEY_REUSED" {
		t.Fatalf("code=%q", p.Code)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("第二阶段不应执行 handler，实际=%d", got)
	}
}

// WriteJSONProblemLite 仅用于断言 problem 的 code 字段。
type WriteJSONProblemLite struct {
	Code string `json:"code"`
}

func TestIdempotencyInvalidKey(t *testing.T) {
	r, calls := idemTestRouter(t)
	w := doPost(r, "bad key!", `{}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法 key 应 400，实际=%d", w.Code)
	}
	if got := atomic.LoadInt32(calls); got != 0 {
		t.Fatalf("非法 key 不应执行 handler，实际=%d", got)
	}
}

func TestIdempotencyNoKeyPassthrough(t *testing.T) {
	r, calls := idemTestRouter(t)
	doPost(r, "", `{}`)
	doPost(r, "", `{}`)
	if got := atomic.LoadInt32(calls); got != 2 {
		t.Fatalf("无 key 应每次执行，实际=%d", got)
	}
}

func TestIdempotencySkipsSafeMethods(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var calls int32
	r := gin.New()
	r.Use(Idempotency(time.Hour))
	r.GET("/api/v1/things", func(c *gin.Context) {
		atomic.AddInt32(&calls, 1)
		c.JSON(200, gin.H{"ok": true})
	})
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/things", nil)
		req.Header.Set(IdempotencyHeader, "key-abcdefgh")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Header().Get(IdempotentReplayHeader) != "" {
			t.Fatal("GET 不应参与幂等重放")
		}
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("GET 执行次数=%d want 2", got)
	}
}

func TestIdempotencyDoesNotCache5xx(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var calls int32
	r := gin.New()
	r.Use(Idempotency(time.Hour))
	r.POST("/api/v1/things", func(c *gin.Context) {
		atomic.AddInt32(&calls, 1)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "boom"})
	})
	doPost(r, "key-abcdefgh", `{}`)
	doPost(r, "key-abcdefgh", `{}`)
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("5xx 不应缓存，实际执行=%d", got)
	}
}

func TestIdempotencyConcurrentSameKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	r := gin.New()
	r.Use(Idempotency(time.Hour))
	r.POST("/api/v1/things", func(c *gin.Context) {
		started <- struct{}{}
		<-release
		c.JSON(http.StatusCreated, gin.H{"ok": true})
	})

	var wg sync.WaitGroup
	wg.Add(1)
	var firstCode int
	go func() {
		defer wg.Done()
		firstCode = doPost(r, "key-abcdefgh", `{}`).Code
	}()
	<-started

	second := doPost(r, "key-abcdefgh", `{}`)
	if second.Code != http.StatusConflict {
		t.Fatalf("并发同 key 应 409，实际=%d", second.Code)
	}
	close(release)
	wg.Wait()
	if firstCode != http.StatusCreated {
		t.Fatalf("首次请求=%d", firstCode)
	}
}

func TestWriteRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var hits int32
	r := gin.New()
	r.Use(WriteRateLimit(2))
	r.POST("/api/v1/things", func(c *gin.Context) {
		atomic.AddInt32(&hits, 1)
		c.Status(http.StatusNoContent)
	})
	r.GET("/api/v1/things", func(c *gin.Context) { c.Status(http.StatusOK) })

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/things", bytes.NewReader(nil))
		req.RemoteAddr = "10.0.0.9:1234"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNoContent {
			t.Fatalf("第 %d 次写请求=%d", i+1, w.Code)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/things", bytes.NewReader(nil))
	req.RemoteAddr = "10.0.0.9:1234"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("超限应 429，实际=%d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("429 应带 Retry-After")
	}
	var p WriteJSONProblemLite
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if p.Code != "WRITE_RATE_LIMITED" {
		t.Fatalf("code=%q body=%s", p.Code, w.Body.String())
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("被限流请求不应到达 handler，实际=%d", got)
	}

	// 读请求不受写限流影响。
	for i := 0; i < 5; i++ {
		greq := httptest.NewRequest(http.MethodGet, "/api/v1/things", nil)
		greq.RemoteAddr = "10.0.0.9:1234"
		gw := httptest.NewRecorder()
		r.ServeHTTP(gw, greq)
		if gw.Code != http.StatusOK {
			t.Fatalf("读请求被误限流: %d", gw.Code)
		}
	}
}
