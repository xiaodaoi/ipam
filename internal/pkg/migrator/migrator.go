// Package migrator 增量迁移执行器（M5-005）。
// 解决 initdb.d 仅在数据卷首次初始化时执行、增量迁移需手工 psql 的问题（M5-004 踩坑）。
// 基线语义：存量库（探测到 0009 产物表）将 ≤baseline 版本记为已应用不重放，仅执行其后增量；
// 全新库从 0001 全量执行。执行走简单协议（pgconn.Exec），支持多语句 .sql 文件。
package migrator

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// baselineThrough 存量库基线版本（runner 引入前最后一批迁移）。
const baselineThrough = "0009"

// legacyProbeTable 基线探测表：0009 的产物，存在即认定存量库。
const legacyProbeTable = "operation_audit"

type migrationFile struct {
	version string // 文件名去 .sql，如 0010_users（零填充前缀，字典序=时间序）
	body    string
}

// listFiles 读取目录下 *.sql 按名升序。
func listFiles(fsys fs.FS) ([]migrationFile, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	out := make([]migrationFile, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		out = append(out, migrationFile{version: strings.TrimSuffix(name, ".sql"), body: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// versionPrefix 提取版本号数字前缀（"0010_users" → "0010"）；序号与基线比较必须用前缀，
// 直接字典序会让 "0009_x" > "0009" 导致基线失效。
func versionPrefix(version string) string {
	if i := strings.Index(version, "_"); i >= 0 {
		return version[:i]
	}
	return version
}

// computePending 待执行计算：已应用跳过；存量基线（空记账+探测命中）跳过 ≤baselineThrough。
func computePending(files []migrationFile, applied map[string]bool, baseline bool) []migrationFile {
	out := []migrationFile{}
	for _, f := range files {
		if applied[f.version] {
			continue
		}
		if baseline && versionPrefix(f.version) <= baselineThrough {
			continue
		}
		out = append(out, f)
	}
	return out
}

// Run 执行增量迁移；dir 为空跳过（纯内存 PoC 模式）。失败返回错误由调用方决定是否 fatal。
func Run(ctx context.Context, pool *pgxpool.Pool, dir string, logf func(string, ...any)) error {
	if dir == "" {
		return nil
	}
	files, err := listFiles(os.DirFS(dir))
	if err != nil {
		return fmt.Errorf("list %s: %w", dir, err)
	}
	if _, err := pool.Exec(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations(
		   version text PRIMARY KEY,
		   applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("schema_migrations: %w", err)
	}
	applied := map[string]bool{}
	rows, err := pool.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	baseline := len(applied) == 0
	if baseline {
		var exists bool
		if err := pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name=$1)`,
			legacyProbeTable).Scan(&exists); err != nil {
			return err
		}
		baseline = exists
	}
	pending := computePending(files, applied, baseline)
	if baseline {
		for _, f := range files {
			if versionPrefix(f.version) <= baselineThrough {
				if err := markApplied(ctx, pool, f.version); err != nil {
					return err
				}
			}
		}
		if logf != nil {
			logf("[migrator] 存量库基线：≤%s 记为已应用（%d 项）", baselineThrough, len(files)-len(pending))
		}
	}
	for _, f := range pending {
		if err := execFile(ctx, pool, f); err != nil {
			return fmt.Errorf("migration %s: %w", f.version, err)
		}
		if err := markApplied(ctx, pool, f.version); err != nil {
			return err
		}
		if logf != nil {
			logf("[migrator] applied %s", f.version)
		}
	}
	if logf != nil && len(pending) == 0 && !baseline {
		logf("[migrator] 无待应用迁移（%d 个文件均已记账）", len(files))
	}
	return nil
}

// execFile 简单协议执行整个 .sql 文件（多语句支持）。
func execFile(ctx context.Context, pool *pgxpool.Pool, f migrationFile) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	_, err = conn.Conn().Exec(ctx, f.body)
	return err
}

func markApplied(ctx context.Context, pool *pgxpool.Pool, version string) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO schema_migrations(version) VALUES($1) ON CONFLICT (version) DO NOTHING`, version)
	return err
}
