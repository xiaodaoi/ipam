// Package backup 备份与恢复（M6-002）：PostgreSQL + 配置目录 + ClickHouse 日志 Parquet。
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	KindManual     = "manual"
	KindAuto       = "auto"
	KindPreUpgrade = "pre_upgrade"

	StatusRunning   = "running"
	StatusSuccess   = "success"
	StatusFailed    = "failed"
	StatusRestoring = "restoring"
)

type Record struct {
	ID         string
	Kind       string
	Status     string
	Note       string
	Dir        string
	SizeBytes  int64
	Checksum   string
	Months     []string
	Error      string
	StartedAt  time.Time
	FinishedAt *time.Time
}

type Settings struct {
	Enabled        bool
	ScheduleHour   int
	RetentionCount int
	UpdatedAt      time.Time
}

type Repo interface {
	Create(ctx context.Context, r *Record) error
	Update(ctx context.Context, r *Record) error
	Get(ctx context.Context, id string) (Record, bool, error)
	List(ctx context.Context, limit int) ([]Record, error)
	Delete(ctx context.Context, id string) error
}

type SettingsRepo interface {
	Get(ctx context.Context) (Settings, error)
	Save(ctx context.Context, s Settings) (Settings, error)
}

// PgRunner PostgreSQL dump/restore（生产实现走 pg_dump/pg_restore）。
type PgRunner interface {
	Dump(ctx context.Context, dst string) (checksum string, size int64, err error)
	Restore(ctx context.Context, src string) error
}

// LogStore ClickHouse 日志导出/恢复（按月 Parquet）。
type LogStore interface {
	Months(ctx context.Context) ([]string, error)
	ExportMonth(ctx context.Context, month, dir string) (rows, size int64, hash string, err error)
	Reset(ctx context.Context) error
	RestoreMonth(ctx context.Context, file string) error
}

type Service struct {
	repo       Repo
	settings   SettingsRepo
	pg         PgRunner
	logs       LogStore
	baseDir    string
	configDirs []string
	now        func() time.Time
}

func NewService(repo Repo, settings SettingsRepo, pg PgRunner, logs LogStore, baseDir string, configDirs []string) *Service {
	if baseDir == "" {
		baseDir = "/srv/log-exports/backups"
	}
	return &Service{repo: repo, settings: settings, pg: pg, logs: logs, baseDir: baseDir,
		configDirs: configDirs, now: time.Now}
}

func (s *Service) List(ctx context.Context, limit int) ([]Record, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.repo.List(ctx, limit)
}

func (s *Service) Get(ctx context.Context, id string) (Record, bool, error) {
	return s.repo.Get(ctx, id)
}

// Create 异步创建备份：先落 running 记录，随后在后台执行。
func (s *Service) Create(ctx context.Context, kind, note string) (Record, error) {
	if kind == "" {
		kind = KindManual
	}
	if kind != KindManual && kind != KindAuto && kind != KindPreUpgrade {
		return Record{}, errors.New("invalid kind")
	}
	id := uuid.NewString()
	rec := Record{
		ID: id, Kind: kind, Status: StatusRunning, Note: note, Months: []string{},
		Dir: filepath.Join(s.baseDir, id), StartedAt: s.now().UTC(),
	}
	if err := s.repo.Create(ctx, &rec); err != nil {
		return Record{}, err
	}
	go s.execute(rec)
	return rec, nil
}

func (s *Service) execute(rec Record) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	fail := func(err error) {
		rec.Status = StatusFailed
		rec.Error = err.Error()
		t := s.now().UTC()
		rec.FinishedAt = &t
		if uerr := s.repo.Update(ctx, &rec); uerr != nil {
			log.Printf("[backup] update failed %s: %v", rec.ID, uerr)
		}
		log.Printf("[backup] %s failed: %v", rec.ID, err)
	}
	if s.pg == nil {
		fail(errors.New("PostgreSQL 备份未配置"))
		return
	}
	if err := os.MkdirAll(rec.Dir, 0o750); err != nil {
		fail(err)
		return
	}
	checksum, size, err := s.pg.Dump(ctx, filepath.Join(rec.Dir, "postgres.dump"))
	if err != nil {
		fail(fmt.Errorf("postgres dump: %w", err))
		return
	}
	rec.Checksum = checksum
	rec.SizeBytes = size

	if err := s.archiveConfig(filepath.Join(rec.Dir, "config.tar.gz")); err != nil {
		fail(fmt.Errorf("config archive: %w", err))
		return
	}
	months := []string{}
	if s.logs != nil {
		var err error
		months, err = s.logs.Months(ctx)
		if err != nil {
			fail(fmt.Errorf("clickhouse months: %w", err))
			return
		}
		sort.Strings(months)
		chDir := filepath.Join(rec.Dir, "clickhouse")
		if err := os.MkdirAll(chDir, 0o750); err != nil {
			fail(err)
			return
		}
		for _, m := range months {
			if _, _, _, err := s.logs.ExportMonth(ctx, m, chDir); err != nil {
				fail(fmt.Errorf("clickhouse export %s: %w", m, err))
				return
			}
		}
	}
	rec.Months = months

	manifest := map[string]any{
		"id": rec.ID, "kind": rec.Kind, "createdAt": rec.StartedAt,
		"postgresChecksum": checksum, "months": months,
		"configDirs": s.configDirs,
	}
	raw, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(rec.Dir, "manifest.json"), raw, 0o640); err != nil {
		fail(err)
		return
	}
	total, err := dirSize(rec.Dir)
	if err != nil {
		fail(err)
		return
	}
	rec.SizeBytes = total
	rec.Status = StatusSuccess
	t := s.now().UTC()
	rec.FinishedAt = &t
	if err := s.repo.Update(ctx, &rec); err != nil {
		log.Printf("[backup] update success %s: %v", rec.ID, err)
	}
	log.Printf("[backup] %s success (%d bytes, %d months)", rec.ID, total, len(months))
}

func (s *Service) archiveConfig(dst string) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, dir := range s.configDirs {
		root := strings.TrimSpace(dir)
		if root == "" {
			continue
		}
		if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
			continue
		}
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			hdr, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			hdr.Name = filepath.Join(filepath.Base(root), rel)
			if info.IsDir() {
				hdr.Name += "/"
			}
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			if info.Mode().IsRegular() {
				src, err := os.Open(path)
				if err != nil {
					return err
				}
				_, cerr := io.Copy(tw, src)
				_ = src.Close()
				return cerr
			}
			return nil
		})
		if err != nil {
			_ = tw.Close()
			_ = gz.Close()
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// Restore 从备份恢复（危险操作，同步执行）。
func (s *Service) Restore(ctx context.Context, id string) (Record, error) {
	rec, ok, err := s.repo.Get(ctx, id)
	if err != nil {
		return Record{}, err
	}
	if !ok {
		return Record{}, errors.New("backup not found")
	}
	if rec.Status != StatusSuccess && rec.Status != StatusFailed {
		return Record{}, errors.New("仅成功/失败的备份可恢复")
	}
	if _, err := os.Stat(filepath.Join(rec.Dir, "postgres.dump")); err != nil {
		return Record{}, fmt.Errorf("备份文件缺失: %w", err)
	}
	rec.Status = StatusRestoring
	rec.Error = ""
	if err := s.repo.Update(ctx, &rec); err != nil {
		return Record{}, err
	}
	finish := func(err error) (Record, error) {
		t := s.now().UTC()
		rec.FinishedAt = &t
		if err != nil {
			rec.Status = StatusFailed
			rec.Error = err.Error()
		} else {
			rec.Status = StatusSuccess
		}
		_ = s.repo.Update(ctx, &rec)
		return rec, err
	}
	if s.pg == nil {
		return finish(errors.New("PostgreSQL 恢复未配置"))
	}
	if err := s.pg.Restore(ctx, filepath.Join(rec.Dir, "postgres.dump")); err != nil {
		return finish(fmt.Errorf("postgres restore: %w", err))
	}
	if s.logs != nil && len(rec.Months) > 0 {
		if err := s.logs.Reset(ctx); err != nil {
			return finish(fmt.Errorf("clickhouse reset: %w", err))
		}
		for _, m := range rec.Months {
			file := filepath.Join(rec.Dir, "clickhouse", "logs-"+m+".parquet")
			if _, err := os.Stat(file); err != nil {
				return finish(fmt.Errorf("日志备份缺失 %s: %w", m, err))
			}
			if err := s.logs.RestoreMonth(ctx, file); err != nil {
				return finish(fmt.Errorf("clickhouse restore %s: %w", m, err))
			}
		}
	}
	return finish(nil)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	rec, ok, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return os.ErrNotExist
	}
	if rec.Status == StatusRunning || rec.Status == StatusRestoring {
		return errors.New("进行中的备份不可删除")
	}
	if rec.Dir != "" && strings.HasPrefix(filepath.Clean(rec.Dir), filepath.Clean(s.baseDir)+string(os.PathSeparator)) {
		if err := os.RemoveAll(rec.Dir); err != nil {
			return err
		}
	}
	return s.repo.Delete(ctx, id)
}

// Cleanup 保留最近 retention 份，删除更旧的记录与文件。
func (s *Service) Cleanup(ctx context.Context, retention int) {
	if retention < 1 {
		return
	}
	list, err := s.repo.List(ctx, 200)
	if err != nil {
		return
	}
	for i, r := range list {
		if i < retention {
			continue
		}
		if r.Status == StatusRunning || r.Status == StatusRestoring {
			continue
		}
		if err := s.Delete(ctx, r.ID); err != nil {
			log.Printf("[backup] cleanup %s: %v", r.ID, err)
		}
	}
}

// StartScheduler 每日自动备份 + 清理（每小时检查一次）。
func StartScheduler(ctx context.Context, svc *Service) {
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		run := func() {
			s, err := svc.settings.Get(ctx)
			if err != nil {
				return
			}
			now := svc.now().UTC()
			if s.Enabled && now.Hour() == s.ScheduleHour {
				list, _ := svc.repo.List(ctx, 20)
				today := now.Format("2006-01-02")
				exists := false
				for _, r := range list {
					if r.Kind == KindAuto && r.StartedAt.UTC().Format("2006-01-02") == today {
						exists = true
						break
					}
				}
				if !exists {
					if _, err := svc.Create(ctx, KindAuto, "定时备份"); err != nil {
						log.Printf("[backup] auto create: %v", err)
					}
				}
			}
			svc.Cleanup(ctx, s.RetentionCount)
		}
		run()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}

func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info != nil && info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

func sha256File(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
