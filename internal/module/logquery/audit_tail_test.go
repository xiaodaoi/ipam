package logquery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
)

func mkAudit(ts time.Time, atype ActorType, action, resource string) AuditEntry {
	return AuditEntry{TS: ts, ActorType: atype, Actor: "tester",
		Method: http.MethodPost, Path: "/api/v1" + resource,
		Action: action, Resource: resource, Status: 201}
}

func TestMemAuditStoreAppendQuery(t *testing.T) {
	s := NewMemAuditStore()
	base := time.Date(2026, 8, 27, 8, 0, 0, 0, time.UTC)
	for i := 5; i >= 1; i-- { // 乱序插入
		e := mkAudit(base.Add(-time.Duration(i)*time.Minute), ActorHuman, "create", "/subnets")
		if i == 2 {
			e.ActorType = ActorBot
			e.TokenSub = "bot:ci#deadbeef"
		}
		if _, err := s.Append(context.Background(), e); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	page, err := s.Query(context.Background(), AuditFilter{From: base.Add(-30 * time.Minute)})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if page.Total != 5 || len(page.Items) != 5 {
		t.Fatalf("total=%d items=%d, want 5/5", page.Total, len(page.Items))
	}
	if page.Items[0].ID < page.Items[4].ID {
		t.Fatalf("应按 (ts,id) DESC 排序")
	}

	botPage, _ := s.Query(context.Background(), AuditFilter{
		From: base.Add(-30 * time.Minute), ActorType: ActorBot})
	if len(botPage.Items) != 1 || botPage.Items[0].TokenSub != "bot:ci#deadbeef" {
		t.Fatalf("actorType 过滤异常: %+v", botPage.Items)
	}

	qPage, _ := s.Query(context.Background(), AuditFilter{
		From: base.Add(-30 * time.Minute), Q: "subnet"})
	if qPage.Total != 5 {
		t.Fatalf("resource 子串过滤 total=%d, want 5", qPage.Total)
	}
}

func TestMemAuditCursorPagination(t *testing.T) {
	s := NewMemAuditStore()
	base := time.Date(2026, 8, 27, 8, 0, 0, 0, time.UTC)
	for i := 0; i < 7; i++ {
		e := mkAudit(base.Add(time.Duration(i)*time.Second), ActorHuman, "update", "/upstreams")
		_, _ = s.Append(context.Background(), e)
	}

	var seen []int64
	cur := ""
	for {
		page, err := s.Query(context.Background(), AuditFilter{
			From: base.Add(-time.Minute), Cursor: cur, PageSize: 3})
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		for _, e := range page.Items {
			seen = append(seen, e.ID)
		}
		if page.NextCursor == "" {
			break
		}
		cur = page.NextCursor
	}
	if len(seen) != 7 {
		t.Fatalf("翻页共 %d 条, want 7", len(seen))
	}
	uniq := map[int64]bool{}
	for _, id := range seen {
		if uniq[id] {
			t.Fatalf("重复 ID %d", id)
		}
		uniq[id] = true
	}
}

func TestAuditRecorderMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewMemAuditStore()
	r := gin.New()
	r.Use(NewAuditRecorder(store))
	r.POST("/api/v1/subnets", func(c *gin.Context) { c.JSON(201, gin.H{"ok": true}) })
	r.GET("/api/v1/ledger", func(c *gin.Context) { c.Status(200) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/subnets", strings.NewReader(`{}`)))
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/api/v1/ledger", nil))

	page, _ := store.Query(context.Background(), AuditFilter{From: time.Now().Add(-time.Hour)})
	if page.Total != 1 {
		t.Fatalf("仅变更类入库: total=%d", page.Total)
	}
	e := page.Items[0]
	if e.Action != "create" || e.Resource != "/subnets" || e.Status != 201 {
		t.Fatalf("审计字段异常: %+v", e)
	}
	if e.ActorType != ActorSystem || e.Actor != "control-plane" {
		t.Fatalf("默认 actor 异常: %s/%s", e.ActorType, e.Actor)
	}
}

// ————— SSE live-tail —————

func TestStreamLogTailSmoke(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mem := NewMemStore()
	h := NewHandler(NewService(mem, nil))
	h.TailPoll = 20 * time.Millisecond
	h.TailHeartbeat = time.Hour // 测试期不触发心跳

	r := gin.New()
	r.GET("/api/v1/logs/tail", func(c *gin.Context) {
		params := apigen.StreamLogTailParams{}
		_ = c.ShouldBindQuery(&params)
		c.Request = c.Request.Clone(c.Request.Context())
		h.StreamLogTail(c, params)
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/logs/tail?from="+time.Now().UTC().Add(-time.Second).Format(time.RFC3339Nano), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type=%s", ct)
	}

	// 连接建立后注入事件（ts 略新于 from 窗口）
	go func() {
		time.Sleep(60 * time.Millisecond)
		mem.Append(LogRow{TS: time.Now().UTC(), Type: "dhcp",
			ClientMAC: "aabbccddee77", ClientIP: "10.61.172.90", Action: "lease_commit"})
	}()

	buf := make([]byte, 4096)
	var got string
	deadline := time.After(2500 * time.Millisecond)
	for !strings.Contains(got, "aabbccddee77") {
		select {
		case <-deadline:
			t.Fatalf("2.5s 内未收到事件，已收: %q", got)
		default:
		}
		n, rerr := resp.Body.Read(buf)
		if rerr != nil && n == 0 {
			t.Fatalf("read: %v", rerr)
		}
		got += string(buf[:n])
		if ctx.Err() != nil {
			break
		}
	}
	if !strings.Contains(got, "id: ") || !strings.Contains(got, `event: log`) {
		t.Fatalf("SSE 帧格式缺失:\n%s", got)
	}
	// data 行为合法 JSON 且含 mac
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "data: ") {
			var row apigen.LogRow
			if jerr := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &row); jerr != nil {
				t.Fatalf("data 非 JSON: %v", jerr)
			}
			break
		}
	}
}

func TestNewerThanTuple(t *testing.T) {
	kts, _, _ := ParseCursor(fmt.Sprintf("%d:aabbccddee01:z.example.", time.UnixMilli(1000).UnixMilli()))
	rows := []LogRow{
		{TS: kts.Add(-time.Millisecond)},                            // 更旧 → 丢弃
		{TS: kts, ClientMAC: "aabbccddee01", Domain: "a.example."},  // 同 ts 更小 domain → 丢弃
		{TS: kts, ClientMAC: "aabbccddee02"},                        // 同 ts 更大 mac → 保留
		{TS: kts.Add(+time.Millisecond), ClientMAC: "aabbccddee00"}, // 更新 ts → 保留
	}
	fresh := newerThan(rows, fmt.Sprintf("%d:aabbccddee01:z.example.", kts.UnixMilli()))
	if len(fresh) != 2 {
		t.Fatalf("want 2 fresh, got %d", len(fresh))
	}
}
