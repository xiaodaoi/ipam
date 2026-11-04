package dns

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestBlockPageDefaultsAndEscaping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/blocked", BlockPageHandler(nil))

	req := httptest.NewRequest(http.MethodGet, "/blocked", nil)
	req.Host = `evil<script>.example.com`
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type=%q", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, defaultBlockTitle) || !strings.Contains(body, defaultBlockMessage) {
		t.Fatalf("应使用默认文案:\n%s", body)
	}
	if strings.Contains(body, "<script>") {
		t.Fatalf("Host 未转义:\n%s", body)
	}
	if !strings.Contains(body, "evil&lt;script&gt;.example.com") {
		t.Fatalf("缺少转义后的访问目标:\n%s", body)
	}
}

func TestBlockPageUsesSettings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := NewMemSettingsRepo()
	raw, _ := json.Marshal(Settings{BlockPageTitle: "公司网络策略", BlockPageMessage: "请提交工单申请"})
	if err := repo.Set(context.Background(), "cache", raw); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.GET("/blocked", BlockPageHandler(repo))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/blocked", nil))
	body := w.Body.String()
	if !strings.Contains(body, "公司网络策略") || !strings.Contains(body, "请提交工单申请") {
		t.Fatalf("未使用设置文案:\n%s", body)
	}
}
