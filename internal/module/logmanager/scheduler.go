package logmanager

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"
)

// StartScheduler 启动日志生命周期后台任务：启动即应用 TTL；每小时检查自动导出与过期归档清理。
func StartScheduler(ctx context.Context, h *LogHandler, admin *ChAdmin) {
	go func() {
		if admin != nil {
			if s, err := h.settings.Get(ctx); err == nil {
				if err := admin.SetTTL(ctx, s.RetentionDays); err != nil {
					log.Printf("logmanager: apply ttl on start: %v", err)
				}
			}
		}
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		runOnce(ctx, h, admin)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runOnce(ctx, h, admin)
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
	now := time.Now()
	if s.AutoExport && admin != nil && now.Day() == 1 && now.Hour() >= 2 {
		prev := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0)
		if _, gerr := h.archives.Get(ctx, prev); gerr != nil {
			autoExport(ctx, h, admin, prev)
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
