package logmanager

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"

	rtypes "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

var monthRe = regexp.MustCompile(`^\d{4}-\d{2}$`)

// Handler 日志存储策略与归档管理（system 域）。
type LogHandler struct {
	settings  SettingsRepo
	archives  ArchiveRepo
	admin     *ChAdmin
	exportDir string
}

func NewLogHandler(settings SettingsRepo, archives ArchiveRepo, admin *ChAdmin, exportDir string) *LogHandler {
	return &LogHandler{settings: settings, archives: archives, admin: admin, exportDir: exportDir}
}

func parseMonth(s string) (time.Time, error) {
	if !monthRe.MatchString(s) {
		return time.Time{}, fmt.Errorf("month must be YYYY-MM, got %q", s)
	}
	return time.Parse("2006-01", s)
}

func badRequest(c *gin.Context, detail string) {
	problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", detail)
}

func internalErr(c *gin.Context, err error) {
	problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
}

func chUnavailable(c *gin.Context) {
	problem.Write(c, http.StatusServiceUnavailable, "https://ipam.local/problems/unavailable", "CH_UNAVAILABLE", "ClickHouse 未配置")
}

func toArchive(a Archive) rtypes.LogArchive {
	out := rtypes.LogArchive{
		Month:      a.Month.UTC().Format("2006-01"),
		RowCount:   a.RowCount,
		FileSize:   a.FileSize,
		ExportedBy: a.ExportedBy,
		CreatedAt:  a.CreatedAt,
	}
	if a.FileHash != "" {
		hash := a.FileHash
		out.FileHash = &hash
	}
	return out
}

func toSettings(s Settings) rtypes.LogSettings {
	return rtypes.LogSettings{
		RetentionDays:     s.RetentionDays,
		ArchiveKeepMonths: s.ArchiveKeepMonths,
		AutoExport:        s.AutoExport,
	}
}

func (h *LogHandler) GetLogSettings(c *gin.Context) {
	s, err := h.settings.Get(c.Request.Context())
	if err != nil {
		internalErr(c, err)
		return
	}
	c.JSON(http.StatusOK, toSettings(s))
}

func (h *LogHandler) UpdateLogSettings(c *gin.Context) {
	var body rtypes.LogSettingsUpdate
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, err.Error())
		return
	}
	cur, err := h.settings.Get(c.Request.Context())
	if err != nil {
		internalErr(c, err)
		return
	}
	if body.RetentionDays != nil {
		if *body.RetentionDays < 1 || *body.RetentionDays > 3650 {
			badRequest(c, "retentionDays 需在 1-3650 之间")
			return
		}
		cur.RetentionDays = *body.RetentionDays
	}
	if body.ArchiveKeepMonths != nil {
		if *body.ArchiveKeepMonths < 1 || *body.ArchiveKeepMonths > 120 {
			badRequest(c, "archiveKeepMonths 需在 1-120 之间")
			return
		}
		cur.ArchiveKeepMonths = *body.ArchiveKeepMonths
	}
	if body.AutoExport != nil {
		cur.AutoExport = *body.AutoExport
	}
	if err := h.settings.Save(c.Request.Context(), cur); err != nil {
		internalErr(c, err)
		return
	}
	if h.admin != nil {
		if err := h.admin.SetTTL(c.Request.Context(), cur.RetentionDays); err != nil {
			problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "CH_TTL_ERROR", err.Error())
			return
		}
	}
	c.JSON(http.StatusOK, toSettings(cur))
}

func (h *LogHandler) ListLogArchives(c *gin.Context) {
	list, err := h.archives.List(c.Request.Context())
	if err != nil {
		internalErr(c, err)
		return
	}
	items := make([]rtypes.LogArchive, 0, len(list))
	for _, a := range list {
		items = append(items, toArchive(a))
	}
	c.JSON(http.StatusOK, rtypes.LogArchiveList{Items: items})
}

func (h *LogHandler) GetLogStorageStats(c *gin.Context) {
	if h.admin == nil {
		chUnavailable(c)
		return
	}
	totalRows, diskBytes, earliest, parts, err := h.admin.Stats(c.Request.Context())
	if err != nil {
		internalErr(c, err)
		return
	}
	s, serr := h.settings.Get(c.Request.Context())
	if serr != nil {
		s = DefaultSettings()
	}
	partStats := make([]rtypes.LogPartitionStat, 0, len(parts))
	for _, p := range parts {
		partStats = append(partStats, rtypes.LogPartitionStat{Month: p.Month, RowCount: p.RowCount, DiskBytes: p.DiskBytes})
	}
	resp := rtypes.LogStorageStats{
		TotalRows:     totalRows,
		DiskBytes:     diskBytes,
		RetentionDays: s.RetentionDays,
		Partitions:    &partStats,
	}
	if earliest != "" {
		resp.EarliestMonth = &earliest
	}
	c.JSON(http.StatusOK, resp)
}

func (h *LogHandler) ExportLogArchive(c *gin.Context, month string) {
	monthTime, err := parseMonth(month)
	if err != nil {
		badRequest(c, err.Error())
		return
	}
	if h.admin == nil {
		chUnavailable(c)
		return
	}
	m := monthTime.Format("2006-01")
	rowCount, fileSize, hash, err := h.admin.ExportMonth(c.Request.Context(), m, h.exportDir)
	if err != nil {
		internalErr(c, err)
		return
	}
	a := Archive{
		Month:      monthTime,
		RowCount:   rowCount,
		FileSize:   fileSize,
		FilePath:   "logs-" + m + ".parquet",
		FileHash:   hash,
		ExportedBy: "manual",
		CreatedAt:  time.Now().UTC(),
	}
	if err := h.archives.Upsert(c.Request.Context(), a); err != nil {
		internalErr(c, err)
		return
	}
	c.JSON(http.StatusOK, toArchive(a))
}

func (h *LogHandler) DeleteLogArchive(c *gin.Context, month string) {
	monthTime, err := parseMonth(month)
	if err != nil {
		badRequest(c, err.Error())
		return
	}
	if a, gerr := h.archives.Get(c.Request.Context(), monthTime); gerr == nil && a.FilePath != "" {
		_ = os.Remove(filepath.Join(h.exportDir, filepath.Base(a.FilePath)))
	}
	if err := h.archives.Delete(c.Request.Context(), monthTime); err != nil {
		internalErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *LogHandler) DownloadLogArchive(c *gin.Context, month string) {
	monthTime, err := parseMonth(month)
	if err != nil {
		badRequest(c, err.Error())
		return
	}
	a, err := h.archives.Get(c.Request.Context(), monthTime)
	if err != nil {
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "NOT_FOUND", "归档不存在")
		return
	}
	path := filepath.Join(h.exportDir, filepath.Base(a.FilePath))
	if _, serr := os.Stat(path); serr != nil {
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "NOT_FOUND", "归档文件不存在")
		return
	}
	c.FileAttachment(path, "logs-"+monthTime.Format("2006-01")+".parquet")
}
