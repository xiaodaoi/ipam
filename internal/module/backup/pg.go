package backup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PgRunner 用 pg_dump/pg_restore 实现 PostgreSQL 备份恢复。
// 依赖镜像内 postgresql-client（Dockerfile 已安装）。
type PgCommandRunner struct {
	host     string
	port     uint16
	user     string
	password string
	db       string
}

func NewPgCommandRunner(dsn string) (*PgCommandRunner, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	return &PgCommandRunner{
		host: cfg.ConnConfig.Host, port: cfg.ConnConfig.Port,
		user: cfg.ConnConfig.User, password: cfg.ConnConfig.Password,
		db: cfg.ConnConfig.Database,
	}, nil
}

func (p *PgCommandRunner) dump(ctx context.Context, dst string) (string, int64, error) {
	args := []string{
		"--format=custom", "--no-owner", "--no-privileges",
		"--file", dst, "-h", p.host, "-p", strconv.Itoa(int(p.port)),
		"-U", p.user, "-d", p.db,
	}
	cmd := exec.CommandContext(ctx, "pg_dump", args...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+p.password)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", 0, fmt.Errorf("pg_dump: %w: %s", err, out)
	}
	return sha256File(dst)
}

func (p *PgCommandRunner) Dump(ctx context.Context, dst string) (string, int64, error) {
	return p.dump(ctx, dst)
}

func (p *PgCommandRunner) Restore(ctx context.Context, src string) error {
	args := []string{
		"--clean", "--if-exists", "--no-owner", "--no-privileges",
		"-h", p.host, "-p", strconv.Itoa(int(p.port)),
		"-U", p.user, "-d", p.db, src,
	}
	cmd := exec.CommandContext(ctx, "pg_restore", args...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+p.password)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pg_restore: %w: %s", err, out)
	}
	return nil
}
