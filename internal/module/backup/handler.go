package backup

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

type APIHandler struct {
	svc      *Service
	settings SettingsRepo
}

func NewAPIHandler(svc *Service, settings SettingsRepo) *APIHandler {
	return &APIHandler{svc: svc, settings: settings}
}

func writeErr(c *gin.Context, status int, code, detail string) {
	problem.Write(c, status, "https://ipam.local/problems/backup", code, detail)
}

func toGen(rec Record) apigen.Backup {
	out := apigen.Backup{
		Id:         mustUUID(rec.ID),
		Kind:       apigen.BackupKind(rec.Kind),
		Status:     apigen.BackupStatus(rec.Status),
		Note:       &rec.Note,
		StartedAt:  rec.StartedAt,
		SizeBytes:  &rec.SizeBytes,
		Checksum:   &rec.Checksum,
		Error:      &rec.Error,
		FinishedAt: rec.FinishedAt,
	}
	months := make([]string, 0, len(rec.Months))
	months = append(months, rec.Months...)
	out.Months = &months
	return out
}

func mustUUID(s string) openapi_types.UUID {
	var u openapi_types.UUID
	_ = u.UnmarshalText([]byte(s))
	return u
}

func (h *APIHandler) ListBackups(c *gin.Context) {
	items, err := h.svc.List(c.Request.Context(), 100)
	if err != nil {
		writeErr(c, 500, "DB_ERROR", err.Error())
		return
	}
	out := make([]apigen.Backup, 0, len(items))
	for _, r := range items {
		out = append(out, toGen(r))
	}
	c.JSON(http.StatusOK, apigen.BackupList{Items: out})
}

func (h *APIHandler) CreateBackup(c *gin.Context) {
	var body apigen.BackupCreate
	if err := c.ShouldBindJSON(&body); err != nil && !errors.Is(err, io.EOF) {
		writeErr(c, 400, "BAD_REQUEST", err.Error())
		return
	}
	note := ""
	if body.Note != nil {
		note = *body.Note
	}
	rec, err := h.svc.Create(c.Request.Context(), KindManual, note)
	if err != nil {
		writeErr(c, 500, "BACKUP_CREATE_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusAccepted, toGen(rec))
}

func (h *APIHandler) DeleteBackup(c *gin.Context, backupId openapi_types.UUID) {
	if err := h.svc.Delete(c.Request.Context(), backupId.String()); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeErr(c, 404, "NOT_FOUND", "备份不存在")
			return
		}
		writeErr(c, 400, "BACKUP_DELETE_FAILED", err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *APIHandler) RestoreBackup(c *gin.Context, backupId openapi_types.UUID) {
	var body apigen.BackupRestoreRequest
	if err := c.ShouldBindJSON(&body); err != nil || !body.Confirm {
		writeErr(c, 400, "CONFIRM_REQUIRED", "恢复为危险操作，必须显式确认")
		return
	}
	rec, err := h.svc.Restore(c.Request.Context(), backupId.String())
	if err != nil {
		writeErr(c, 500, "RESTORE_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, toGen(rec))
}

func (h *APIHandler) DownloadBackup(c *gin.Context, backupId openapi_types.UUID) {
	rec, ok, err := h.svc.Get(c.Request.Context(), backupId.String())
	if err != nil {
		writeErr(c, 500, "DB_ERROR", err.Error())
		return
	}
	if !ok || rec.Dir == "" || rec.Status == StatusRunning {
		writeErr(c, 404, "NOT_FOUND", "备份不存在或尚未完成")
		return
	}
	if _, err := os.Stat(rec.Dir); err != nil {
		writeErr(c, 404, "FILES_MISSING", "备份文件不存在")
		return
	}
	c.Header("Content-Type", "application/gzip")
	c.Header("Content-Disposition", `attachment; filename="backup-`+rec.ID+`.tar.gz"`)
	if err := streamTarGz(c.Writer, rec.Dir); err != nil {
		// 头已发出时只能记录；前端会看到不完整下载
		return
	}
}

func streamTarGz(w io.Writer, root string) error {
	gz := gzip.NewWriter(w)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = rel
		if info.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			_, cerr := io.Copy(tw, f)
			_ = f.Close()
			return cerr
		}
		return nil
	})
}

func (h *APIHandler) GetBackupSettings(c *gin.Context) {
	s, err := h.settings.Get(c.Request.Context())
	if err != nil {
		writeErr(c, 500, "DB_ERROR", err.Error())
		return
	}
	c.JSON(http.StatusOK, toGenSettings(s))
}

func (h *APIHandler) UpdateBackupSettings(c *gin.Context) {
	var body apigen.BackupSettingsUpdate
	if err := c.ShouldBindJSON(&body); err != nil {
		writeErr(c, 400, "BAD_REQUEST", err.Error())
		return
	}
	if body.ScheduleHour < 0 || body.ScheduleHour > 23 || body.RetentionCount < 1 || body.RetentionCount > 90 {
		writeErr(c, 400, "BAD_REQUEST", "scheduleHour 0-23，retentionCount 1-90")
		return
	}
	s, err := h.settings.Save(c.Request.Context(), Settings{
		Enabled: body.Enabled, ScheduleHour: body.ScheduleHour, RetentionCount: body.RetentionCount,
	})
	if err != nil {
		writeErr(c, 500, "DB_ERROR", err.Error())
		return
	}
	c.JSON(http.StatusOK, toGenSettings(s))
}

func toGenSettings(s Settings) apigen.BackupSettings {
	ts := s.UpdatedAt
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	return apigen.BackupSettings{
		Enabled: s.Enabled, ScheduleHour: s.ScheduleHour,
		RetentionCount: s.RetentionCount, UpdatedAt: &ts,
	}
}
