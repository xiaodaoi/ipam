package logquery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// M6-006：变更前实体快照（脱敏）在 handler 执行前抓取。
func TestAuditRecorderCapturesBeforeSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewMemAuditStore()
	rec := NewAuditRecorder(store)
	rec.RegisterBefore("/subnets/:subnetId", func(_ context.Context, c *gin.Context) (any, error) {
		return map[string]any{
			"id":       c.Param("subnetId"),
			"cidr":     "10.0.0.0/24",
			"name":     "改名前的子网",
			"apiToken": "super-secret",
		}, nil
	})
	r := gin.New()
	r.Use(rec.Middleware())
	r.PATCH("/api/v1/subnets/:subnetId", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPatch, "/api/v1/subnets/sub-1",
		strings.NewReader(`{"name":"新名字"}`)))

	page, err := store.Query(context.Background(), AuditFilter{From: time.Now().Add(-time.Hour)})
	if err != nil || page.Total != 1 {
		t.Fatalf("audit total=%d err=%v", page.Total, err)
	}
	e := page.Items[0]
	if e.Before == "" || !strings.Contains(e.Before, "改名前的子网") {
		t.Fatalf("before 快照缺失: %q", e.Before)
	}
	if strings.Contains(e.Before, "super-secret") {
		t.Fatalf("before 未脱敏: %q", e.Before)
	}
	if !strings.Contains(e.Before, "***") {
		t.Fatalf("before 应包含脱敏占位: %q", e.Before)
	}
	if e.Payload == "" || !strings.Contains(e.Payload, "新名字") {
		t.Fatalf("payload 缺失: %q", e.Payload)
	}

	// 含 before 的记录同样应通过链校验。
	checked, broken, err := store.Verify(context.Background(), time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	if err != nil || broken != 0 || checked != 1 {
		t.Fatalf("verify checked=%d broken=%d err=%v", checked, broken, err)
	}
}

// 未登记资源的变更不产生 before，且不影响记录。
func TestAuditRecorderNoBeforeForUnregistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewMemAuditStore()
	r := gin.New()
	r.Use(NewAuditRecorder(store).Middleware())
	r.POST("/api/v1/dns/zones", func(c *gin.Context) { c.Status(http.StatusCreated) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/dns/zones", strings.NewReader(`{"name":"x"}`)))

	page, _ := store.Query(context.Background(), AuditFilter{From: time.Now().Add(-time.Hour)})
	if page.Total != 1 || page.Items[0].Before != "" {
		t.Fatalf("unregistered before 应为空: %+v", page.Items)
	}
}

// 链哈希对历史（无 before）记录保持兼容：空 before 不参与哈希。
func TestAuditChainHashSkipsEmptyBefore(t *testing.T) {
	e := AuditEntry{ID: 1, TS: time.Unix(0, 0).UTC(), Actor: "a", Method: "POST", Path: "/x", Action: "create", Resource: "/x", Status: 200}
	withEmpty := auditChainHash(e, "")
	e.Before = ""
	if got := auditChainHash(e, ""); got != withEmpty {
		t.Fatalf("空 before 不应改变哈希")
	}
	e.Before = `{"a":1}`
	if got := auditChainHash(e, ""); got == withEmpty {
		t.Fatalf("非空 before 应改变哈希")
	}
}

// 脱敏：文本机密（口令/密钥）打码；布尔策略开关（如 forcePasswordChange）保留可读。
func TestRedactSensitiveKeepsBooleanPolicyFlags(t *testing.T) {
	v := map[string]any{
		"forcePasswordChange": true,
		"password":            "p@ss",
		"totpSecret":          "S3CR3T",
		"nested":              map[string]any{"apiToken": "abc", "enabled": false},
	}
	out, _ := json.Marshal(redactSensitive(v))
	s := string(out)
	if strings.Contains(s, "p@ss") || strings.Contains(s, "S3CR3T") || strings.Contains(s, "abc") {
		t.Fatalf("机密未打码: %s", s)
	}
	if !strings.Contains(s, `"forcePasswordChange":true`) || !strings.Contains(s, `"enabled":false`) {
		t.Fatalf("布尔策略开关不应被打码: %s", s)
	}
}
