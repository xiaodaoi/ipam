package logmanager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	chdriver "github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// ChConfig ClickHouse 连接配置（原生协议用于查询/DDL，HTTP 用于 Parquet 导出）。
type ChConfig struct {
	Addr     string // host:9000（原生协议）
	HTTPAddr string // host:8123（HTTP，导出 Parquet）
	DB       string
	User     string
	Password string
}

// PartStat 单月分区统计（由 system.parts 聚合）。
type PartStat struct {
	Month     string
	RowCount  int64
	DiskBytes int64
}

// ChAdmin ClickHouse 运维操作：TTL 调整 / 分区统计 / 按月 Parquet 导出。
type ChAdmin struct {
	conn     chdriver.Conn
	db       string
	httpAddr string
	user     string
	password string
}

// OpenChAdmin 建立原生连接（HTTP 地址用于导出）。
func OpenChAdmin(cfg ChConfig) (*ChAdmin, error) {
	db := cfg.DB
	if db == "" {
		db = "ipam"
	}
	httpAddr := cfg.HTTPAddr
	if httpAddr == "" {
		httpAddr = deriveHTTPAddr(cfg.Addr)
	}
	opts := &clickhouse.Options{
		Addr: []string{cfg.Addr},
		Auth: clickhouse.Auth{Database: db, Username: cfg.User, Password: cfg.Password},
		Settings: clickhouse.Settings{
			"max_execution_time": 300,
		},
		DialTimeout:  5 * time.Second,
		MaxOpenConns: 3,
		MaxIdleConns: 1,
	}
	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, err
	}
	return &ChAdmin{conn: conn, db: db, httpAddr: httpAddr, user: cfg.User, password: cfg.Password}, nil
}

// deriveHTTPAddr 由原生地址推导 HTTP 地址（:9000 → :8123）。
func deriveHTTPAddr(native string) string {
	if native == "" {
		return ""
	}
	if strings.Contains(native, ":9000") {
		return strings.Replace(native, ":9000", ":8123", 1)
	}
	host := native
	if i := strings.LastIndex(native, ":"); i >= 0 {
		host = native[:i]
	}
	return host + ":8123"
}

// SetTTL 调整 logs 表 TTL（元数据操作，秒级生效）；TopN 物化视图尽力跟随。
func (a *ChAdmin) SetTTL(ctx context.Context, days int) error {
	if days < 1 || days > 3650 {
		return fmt.Errorf("invalid retention days: %d", days)
	}
	q := fmt.Sprintf("ALTER TABLE %s.logs MODIFY TTL toDateTime(ts) + INTERVAL %d DAY", a.db, days)
	if err := a.conn.Exec(ctx, q); err != nil {
		return fmt.Errorf("set ttl on logs: %w", err)
	}
	a.setMVTTL(ctx, days)
	return nil
}

// setMVTTL 让 logs_topn_hourly 的 TTL 跟随主表。物化视图内表由 ClickHouse 内部命名为
// .inner_id.<uuid>，需动态解析；任何失败仅记录日志，不影响主表 TTL 生效。
func (a *ChAdmin) setMVTTL(ctx context.Context, days int) {
	var inner string
	err := a.conn.QueryRow(ctx,
		`SELECT concat('.inner_id.', toString(uuid)) FROM system.tables WHERE database = ? AND name = 'logs_topn_hourly'`,
		a.db).Scan(&inner)
	if err != nil || inner == "" {
		log.Printf("logmanager: resolve mv inner table: %v", err)
		return
	}
	q := fmt.Sprintf("ALTER TABLE %s.`%s` MODIFY TTL hour + INTERVAL %d DAY", a.db, inner, days)
	if err := a.conn.Exec(ctx, q); err != nil {
		log.Printf("logmanager: set ttl on mv inner table: %v", err)
	}
}

// Stats 实时统计：总行数 / 总磁盘 / 最早月份 / 按月分区明细。
func (a *ChAdmin) Stats(ctx context.Context) (totalRows, diskBytes int64, earliestMonth string, parts []PartStat, err error) {
	rows, err := a.conn.Query(ctx,
		`SELECT substring(partition, 1, 7) AS m,
		        toInt64(sum(rows)) AS r, toInt64(sum(bytes_on_disk)) AS b
		   FROM system.parts
		  WHERE database = ? AND table = 'logs' AND active
		  GROUP BY m ORDER BY m DESC`, a.db)
	if err != nil {
		return 0, 0, "", nil, err
	}
	defer func() { _ = rows.Close() }()
	parts = make([]PartStat, 0)
	for rows.Next() {
		var p PartStat
		if err := rows.Scan(&p.Month, &p.RowCount, &p.DiskBytes); err != nil {
			return 0, 0, "", nil, err
		}
		totalRows += p.RowCount
		diskBytes += p.DiskBytes
		earliestMonth = p.Month
		parts = append(parts, p)
	}
	return totalRows, diskBytes, earliestMonth, parts, rows.Err()
}

// ExportMonth 导出某月日志为 Parquet（HTTP 接口 + FORMAT Parquet，无需 FILE 权限）。
// 返回行数 / 文件大小 / SHA256。
func (a *ChAdmin) ExportMonth(ctx context.Context, month, dir string) (int64, int64, string, error) {
	start, err := time.Parse("2006-01", month)
	if err != nil {
		return 0, 0, "", fmt.Errorf("invalid month %q: %w", month, err)
	}
	end := start.AddDate(0, 1, 0)

	var rowCount int64
	if err := a.conn.QueryRow(ctx,
		`SELECT toInt64(count()) FROM `+a.db+`.logs WHERE ts >= ? AND ts < ?`, start, end).Scan(&rowCount); err != nil {
		return 0, 0, "", fmt.Errorf("count month rows: %w", err)
	}

	filename := "logs-" + month + ".parquet"
	path := filepath.Join(dir, filename)
	f, err := os.Create(path)
	if err != nil {
		return 0, 0, "", fmt.Errorf("create export file: %w", err)
	}
	defer func() { _ = f.Close() }()

	query := fmt.Sprintf(`SELECT * FROM %s.logs WHERE ts >= '%s' AND ts < '%s' FORMAT Parquet`,
		a.db, start.Format("2006-01-02 15:04:05"), end.Format("2006-01-02 15:04:05"))
	url := fmt.Sprintf("http://%s/?database=%s&max_execution_time=600", a.httpAddr, a.db)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(query))
	if err != nil {
		return 0, 0, "", err
	}
	req.Header.Set("X-ClickHouse-User", a.user)
	req.Header.Set("X-ClickHouse-Key", a.password)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, 0, "", fmt.Errorf("ch http export: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return 0, 0, "", fmt.Errorf("ch http export status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), resp.Body)
	if err != nil {
		return 0, 0, "", fmt.Errorf("write export file: %w", err)
	}
	return rowCount, n, hex.EncodeToString(h.Sum(nil)), nil
}
