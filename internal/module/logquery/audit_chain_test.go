package logquery

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestMemAuditChainVerify(t *testing.T) {
	store := NewMemAuditStore()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := store.Append(ctx, AuditEntry{
			ActorType: ActorHuman, Actor: "admin", Method: "POST", Path: "/api/v1/x",
			Action: "create", Resource: "/x", Status: 200, Detail: "ok",
		}); err != nil {
			t.Fatal(err)
		}
	}
	from := time.Now().Add(-time.Minute)
	checked, broken, err := store.Verify(ctx, from, time.Now().Add(time.Minute))
	if err != nil || broken != 0 || checked != 3 {
		t.Fatalf("verify: checked=%d broken=%d err=%v", checked, broken, err)
	}
	store.rows[1].Payload = `{"tampered":true}`
	checked, broken, err = store.Verify(ctx, from, time.Now().Add(time.Minute))
	if err != nil || broken != store.rows[1].ID || checked != 1 {
		t.Fatalf("tamper detect: checked=%d broken=%d err=%v", checked, broken, err)
	}
}

func TestCapturePayloadRedactsAndRestores(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"username":"admin","password":"secret","nested":{"token":"abc"},"ok":1}`
	req := httptest.NewRequest("POST", "/api/v1/users", strings.NewReader(body))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	got := capturePayload(c)
	if !strings.Contains(got, `"password":"***"`) || !strings.Contains(got, `"token":"***"`) {
		t.Fatalf("payload not redacted: %s", got)
	}
	restored, _ := io.ReadAll(c.Request.Body)
	if string(restored) != body {
		t.Fatalf("body not restored: %s", restored)
	}
}
