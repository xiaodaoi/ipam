package logquery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/xiaodaoi/ipam/internal/pkg/auditmeta"
)

type memAuditStore struct{ items []AuditEntry }

func (m *memAuditStore) Append(_ context.Context, e AuditEntry) (AuditEntry, error) {
	e.ID = int64(len(m.items) + 1)
	m.items = append(m.items, e)
	return e, nil
}

func (m *memAuditStore) Query(context.Context, AuditFilter) (AuditPage, error) {
	return AuditPage{Items: m.items, Total: len(m.items)}, nil
}

func TestAuditRecorderUsesContextMeta(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &memAuditStore{}
	r := gin.New()
	r.Use(NewAuditRecorder(store))
	r.POST("/api/v1/auth/login", func(c *gin.Context) {
		c.Set(auditmeta.ActionKey, "login")
		c.Set(auditmeta.ActorKey, "alice")
		c.Set(auditmeta.DetailKey, "failed")
		c.Status(http.StatusUnauthorized)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil))
	if len(store.items) != 1 {
		t.Fatalf("应记录 1 条审计: %d", len(store.items))
	}
	e := store.items[0]
	if e.Action != "login" || e.Actor != "alice" || e.Detail != "failed" || e.Status != 401 {
		t.Fatalf("审计上下文未生效: %+v", e)
	}
}
