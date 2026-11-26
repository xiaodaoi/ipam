package alerts

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestUpsertJustNotifiedNullSafe 复现并锁定一个真实缺陷（M6-049 修复）：
// 新建 notify=false 的告警时 last_notified_at 为 NULL，若 RETURNING 直接写
// `last_notified_at = now()`，PostgreSQL 返回 NULL，扫进 bool 会报
// "cannot scan NULL into *bool"，导致整轮 DNS 风险访问采集中断。
//
// 需要真实 PostgreSQL（已应用 0054 迁移），通过 IPAM_TEST_PG_DSN 启用；
// 未设置时跳过，不阻塞普通 go test。
func TestUpsertJustNotifiedNullSafe(t *testing.T) {
	dsn := os.Getenv("IPAM_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("IPAM_TEST_PG_DSN 未设置，跳过 PostgreSQL 集成测试")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("连接 PostgreSQL 失败: %v", err)
	}
	defer pool.Close()

	repo := NewPgRepo(pool)
	now := time.Now().UTC().Truncate(time.Millisecond)
	base := "test:just-notified:" + now.Format("20060102150405.000000000")
	cleanup := func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM alert WHERE dedup_key LIKE $1`, base+"%")
	}
	cleanup()
	defer cleanup()

	event := func(key string, notify bool) Event {
		return Event{
			DedupKey:    key,
			Fingerprint: key,
			Source:      "dns",
			Kind:        "risk_domain",
			Severity:    "info",
			Title:       "情报源聚合归档",
			Message:     "验证 just_notified NULL 安全",
			AlertClass:  "intel_feed",
			Notify:      notify,
			Count:       1,
			FirstSeen:   now,
			LastSeen:    now,
			WindowStart: now,
		}
	}

	// notify=false：last_notified_at 保持 NULL，不应误判为“刚刚通知”。
	quietKey := base + ":quiet"
	got, err := repo.Upsert(ctx, event(quietKey, false))
	if err != nil {
		t.Fatalf("notify=false 的 Upsert 不应失败（NULL 扫描回归）: %v", err)
	}
	if got.JustNotified {
		t.Fatalf("notify=false 不应返回 JustNotified=true")
	}
	if got.LastNotifiedAt != nil {
		t.Fatalf("notify=false 不应写入 last_notified_at，得到 %v", got.LastNotifiedAt)
	}

	// notify=true 的新告警：本轮应标记 JustNotified，用于消耗每小时通知预算。
	notifyKey := base + ":notify"
	got, err = repo.Upsert(ctx, event(notifyKey, true))
	if err != nil {
		t.Fatalf("notify=true 的 Upsert 失败: %v", err)
	}
	if !got.JustNotified {
		t.Fatalf("首次 notify=true 应返回 JustNotified=true")
	}
	if got.LastNotifiedAt == nil {
		t.Fatalf("首次 notify=true 应写入 last_notified_at")
	}

	// 同窗口重复扫描：在重复提醒周期内不应再次消耗通知预算。
	got, err = repo.Upsert(ctx, event(notifyKey, true))
	if err != nil {
		t.Fatalf("重复扫描的 Upsert 失败: %v", err)
	}
	if got.JustNotified {
		t.Fatalf("重复提醒周期内不应重复返回 JustNotified=true")
	}
}
