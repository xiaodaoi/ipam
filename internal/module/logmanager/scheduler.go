package logmanager

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// StartScheduler 启动日志生命周期后台任务：启动即对账 TTL；每小时对账 TTL 并补导出。
func StartScheduler(ctx context.Context, h *LogHandler, admin *ChAdmin) {
	reconcile := func() {
		if admin != nil {
			if s, err := h.settings.Get(ctx); err == nil {
				if err := admin.EnsureTTL(ctx, s.RetentionDays); err != nil {
					log.Printf("logmanager: reconcile ttl: %v", err)
				}
			} else {
				log.Printf("logmanager: load settings for ttl: %v", err)
			}
		}
		runOnce(ctx, h, admin)
	}
	go func() {
		reconcile()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				reconcile()
			}
		}
	}()
}

func runOnce(ctx context.Context, h *LogHandler, admin *ChAdmin) {
	s, err := h.settings.Get(ctx)
	if err != nil {
		log.Printf("logmanager: load settings: %v", err)
		return
	}
	now := time.Now().UTC()
	if s.AutoExport && admin != nil {
		// 补偿式导出（M6-066）：扫描所有「已结束且未归档」的月份，而不是只在
		// 每月 1 日 02:00 触发——服务当天宕机就会永久缺一个月的归档。
		months, merr := admin.Months(ctx)
		if merr != nil {
			log.Printf("logmanager: list months for export: %v", merr)
		} else {
			for i, month := range pendingArchiveMonths(months, now, func(m time.Time) bool {
				_, gerr := h.archives.Get(ctx, m)
				return gerr == nil
			}) {
				if i >= 3 { // 单轮最多补 3 个月，避免小时任务长时间占用
					break
				}
				autoExport(ctx, h, admin, month)
			}
		}
	}
	cleanupArchives(ctx, h, s.ArchiveKeepMonths)
}

func autoExport(ctx context.Context, h *LogHandler, admin *ChAdmin, month time.Time) {
	m := month.Format("2006-01")
	rowCount, fileSize, hash, err := admin.ExportMonth(ctx, m, h.exportDir)
	if err != nil {
		log.Printf("logmanager: auto export %s: %v", m, err)
		return
	}
	a := Archive{
		Month:      month,
		RowCount:   rowCount,
		FileSize:   fileSize,
		FilePath:   "logs-" + m + ".parquet",
		FileHash:   hash,
		ExportedBy: "system",
		CreatedAt:  time.Now().UTC(),
	}
	if err := h.archives.Upsert(ctx, a); err != nil {
		log.Printf("logmanager: save archive %s: %v", m, err)
		return
	}
	log.Printf("logmanager: auto exported %s (%d rows, %d bytes)", m, rowCount, fileSize)
}

func cleanupArchives(ctx context.Context, h *LogHandler, keepMonths int) {
	if keepMonths < 1 {
		return
	}
	list, err := h.archives.List(ctx)
	if err != nil {
		log.Printf("logmanager: list archives: %v", err)
		return
	}
	now := time.Now().UTC()
	cutoff := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -keepMonths, 0)
	for _, a := range list {
		if !a.Month.UTC().Before(cutoff) {
			continue
		}
		if a.FilePath != "" {
			_ = os.Remove(filepath.Join(h.exportDir, filepath.Base(a.FilePath)))
		}
		if err := h.archives.Delete(ctx, a.Month); err != nil {
			log.Printf("logmanager: delete archive %s: %v", a.Month.Format("2006-01"), err)
			continue
		}
		log.Printf("logmanager: cleaned expired archive %s", a.Month.Format("2006-01"))
	}
}

// pendingArchiveMonths 返回「已结束且尚未归档」的月份（升序）。
// 已结束 = 早于当前 UTC 月；have(m) 返回 true 表示已有归档记录。
func pendingArchiveMonths(months []string, now time.Time, have func(time.Time) bool) []time.Time {
	cur := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	var out []time.Time
	for _, raw := range months {
		m, err := time.Parse("2006-01", raw)
		if err != nil || !m.Before(cur) {
			continue
		}
		if have(m) {
			continue
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}
