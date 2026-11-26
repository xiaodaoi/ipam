package alerts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestDispatcher_Notify不阻塞采集（M6-056）：通道慢/卡死时 Notify 必须立即返回，
// 由后台 worker 带超时与重试完成投递，否则 DNS 采集/系统探针会整体停摆。
func TestDispatcher_Notify不阻塞采集(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	repo := NewMemRepo()
	if _, err := repo.CreateChannel(context.Background(), Channel{
		ID: "c1", Name: "slow", Type: ChannelWebhook,
		Config: ChannelConfig{URL: srv.URL}, MinSeverity: SeverityInfo, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	d := NewDispatcher(repo, "", "", nil)
	d.sendTimeout = 150 * time.Millisecond
	d.retries = 1

	start := time.Now()
	d.Notify(context.Background(), Alert{
		ID: "a1", Notify: true, Severity: SeverityCritical, Source: SourceSystem,
		Kind: "test", Title: "t", Message: "m", Count: 1, LastSeen: time.Now(),
	})
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("Notify 阻塞了采集循环: %v", elapsed)
	}

	// worker 异步完成：超时(150ms) + 退避(1s) + 第二次超时(150ms)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if list, total, err := repo.ListDeliveries(context.Background(), DeliveryFilter{Limit: 10}); err == nil && total > 0 {
			if len(list) != 1 {
				t.Fatalf("want 1 delivery, got %d", len(list))
			}
			if list[0].Status != "failed" {
				t.Fatalf("status = %q, want failed", list[0].Status)
			}
			if list[0].Attempt != 2 {
				t.Fatalf("attempt = %d, want 2（1 次 + 1 次重试）", list[0].Attempt)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("投递记录未在超时内写入")
}

// TestPurgeDeliveries 保留期清理（M6-056）。
func TestPurgeDeliveries(t *testing.T) {
	repo := NewMemRepo()
	ctx := context.Background()
	now := time.Now().UTC()
	for i, at := range []time.Time{now.Add(-100 * 24 * time.Hour), now.Add(-10 * 24 * time.Hour)} {
		if err := repo.CreateDelivery(ctx, Delivery{
			ID: string(rune('a' + i)), AlertID: "x", ChannelID: "c", ChannelName: "c",
			Status: "sent", SentAt: at,
		}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := repo.PurgeDeliveries(ctx, now.Add(-90*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("purged %d, want 1", n)
	}
	if _, total, _ := repo.ListDeliveries(ctx, DeliveryFilter{}); total != 1 {
		t.Fatalf("remaining %d, want 1", total)
	}
}
