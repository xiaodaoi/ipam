package logmanager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	chdriver "github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const (
	// DefaultExportTimeout 单个日志月份导出/恢复的默认预算。
	// 单月日志可达数千万行、数百 MB Parquet，并发负载下 Parquet 编码可能超过 10 分钟；
	// 旧实现固定 5 分钟客户端超时、服务端却允许 600s，两者矛盾导致备份被误杀（M6-028）。
	DefaultExportTimeout = 30 * time.Minute
	// exportServerMargin 服务端预算比客户端 context 略短，
	// 保证超时先由 ClickHouse 返回明确错误（而不是客户端先放弃、服务端仍在跑）。
	exportServerMargin = 30 * time.Second
)

// chHTTPClient 日志导出/恢复专用。
// 不设置 http.Client.Timeout：它同时约束「等待响应头」和「读取大响应体」，
// 预算统一由每次请求的 context 控制（见 ChAdmin.exportTimeout）。
// 连接级超时仍然保留，避免对端不响应 TCP/TLS 时长时间占用连接。
var chHTTPClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
		MaxIdleConns:          4,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       90 * time.Second,
	},
}

// ChConfig ClickHouse 连接配置（原生协议用于查询/DDL，HTTP 用于 Parquet 导出/恢复）。
type ChConfig struct {
	Addr     string // host:9000（原生协议）
	HTTPAddr string // host:8123（HTTP，导出 Parquet）
	DB       string
	User     string
	Password string
	// ExportTimeout 单月导出/恢复预算；<=0 时使用 DefaultExportTimeout。
	ExportTimeout time.Duration
}

// PartStat 单月分区统计（由 system.parts 聚合）。
type PartStat struct {
	Month     string
	RowCount  int64
	DiskBytes int64
}

// ChAdmin ClickHouse 运维操作：TTL 调整 / 分区统计 / 按月 Parquet 导出。
type ChAdmin struct {
	conn          chdriver.Conn
	db            string
	httpAddr      string
	user          string
	password      string
	exportTimeout time.Duration
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
	exportTimeout := cfg.ExportTimeout
	if exportTimeout <= 0 {
		exportTimeout = DefaultExportTimeout
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
	return &ChAdmin{
		conn: conn, db: db, httpAddr: httpAddr,
		user: cfg.User, password: cfg.Password, exportTimeout: exportTimeout,
	}, nil
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
// Ping 连通性探针（/metrics 组件健康用）。
func (a *ChAdmin) Ping(ctx context.Context) error {
	return a.conn.Ping(ctx)
}

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
	return a.exportRange(ctx, month, start, end, dir, rowCount)
}

// exportRange 通过 ClickHouse HTTP 接口把 [start,end) 的日志写成 Parquet。
// 预算由 exportTimeout 统一控制（客户端 context 与 URL 的 max_execution_time 同源）；
// 失败/超时会删除半成品文件，避免备份目录留下不可用的 Parquet。
func (a *ChAdmin) exportRange(ctx context.Context, month string, start, end time.Time, dir string, rowCount int64) (rows, size int64, sum string, err error) {
	budget := a.exportTimeout
	if budget <= 0 {
		budget = DefaultExportTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	path := filepath.Join(dir, "logs-"+month+".parquet")
	f, err := os.Create(path)
	if err != nil {
		return 0, 0, "", fmt.Errorf("create export file: %w", err)
	}
	defer func() {
		_ = f.Close()
		if err != nil {
			_ = os.Remove(path)
		}
	}()

	// 服务端预算略短于客户端，确保超时先返回 ClickHouse 的明确错误。
	serverBudget := int(budget / time.Second)
	if margin := int(exportServerMargin / time.Second); serverBudget > margin {
		serverBudget -= margin
	}
	if serverBudget < 1 {
		serverBudget = 1
	}

	query := fmt.Sprintf(`SELECT * FROM %s.logs WHERE ts >= '%s' AND ts < '%s' FORMAT Parquet`,
		a.db, start.Format("2006-01-02 15:04:05"), end.Format("2006-01-02 15:04:05"))
	url := fmt.Sprintf("http://%s/?database=%s&max_execution_time=%d", a.httpAddr, a.db, serverBudget)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(query))
	if err != nil {
		return 0, 0, "", err
	}
	req.Header.Set("X-ClickHouse-User", a.user)
	req.Header.Set("X-ClickHouse-Key", a.password)
	resp, err := chHTTPClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return 0, 0, "", fmt.Errorf(
				"日志导出 %s 超时（预算 %s）：请检查 ClickHouse 负载，或调大 IPAM_CH_EXPORT_TIMEOUT: %w",
				month, budget, err)
		}
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
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return 0, 0, "", fmt.Errorf(
				"日志导出 %s 超时（预算 %s）：请检查 ClickHouse 负载，或调大 IPAM_CH_EXPORT_TIMEOUT: %w",
				month, budget, err)
		}
		return 0, 0, "", fmt.Errorf("write export file: %w", err)
	}
	if err := checkParquetComplete(f); err != nil {
		return 0, 0, "", fmt.Errorf("日志导出 %s: %w", month, err)
	}
	return rowCount, n, hex.EncodeToString(h.Sum(nil)), nil
}

// checkParquetComplete 校验 Parquet 文件是否完整落盘。
//
// ClickHouse 在**已发送 200 响应头之后**发生运行时错误（最典型的是 max_execution_time 超时）
// 时，不会修改 HTTP 状态码，而是把异常文本追加到响应体末尾。若只判断 StatusCode，
// 会把「半截数据 + 错误文本」当作成功备份（M6-028 实测：504MB 的文件被截断成 24MB 仍报 success）。
// Parquet 文件以 4 字节 "PAR1" 结尾，用它可低成本识别截断，并回读尾部异常文本便于定位。
func checkParquetComplete(f *os.File) error {
	const magic = "PAR1"
	if _, err := f.Seek(-int64(len(magic)), io.SeekEnd); err != nil {
		return fmt.Errorf("导出文件过小或不可读: %w", err)
	}
	buf := make([]byte, len(magic))
	if _, err := io.ReadFull(f, buf); err != nil {
		return fmt.Errorf("读取导出文件尾部失败: %w", err)
	}
	if string(buf) == magic {
		return nil
	}
	hint := ""
	if st, serr := f.Stat(); serr == nil && st.Size() > 0 {
		off := st.Size() - 2048
		if off < 0 {
			off = 0
		}
		if _, serr := f.Seek(off, io.SeekStart); serr == nil {
			tail, _ := io.ReadAll(io.LimitReader(f, 2048))
			if i := strings.Index(string(tail), "Code: "); i >= 0 {
				hint = ": " + strings.TrimSpace(string(tail[i:]))
				if len(hint) > 300 {
					hint = hint[:300] + "…"
				}
			}
		}
	}
	return fmt.Errorf("导出文件不完整（缺少 Parquet 结束标记，常见于 ClickHouse 流式响应中途超时）%s", hint)
}

// Months 返回 logs 表存在数据的月份（升序）。
func (a *ChAdmin) Months(ctx context.Context) ([]string, error) {
	_, _, _, parts, err := a.Stats(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p.RowCount > 0 {
			out = append(out, p.Month)
		}
	}
	return out, nil
}

func (a *ChAdmin) httpQuery(ctx context.Context, query string, body io.Reader) error {
	if a.httpAddr == "" {
		return fmt.Errorf("clickhouse HTTP 地址未配置")
	}
	budget := a.exportTimeout
	if budget <= 0 {
		budget = DefaultExportTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.httpAddr, body)
	if err != nil {
		return err
	}
	q := req.URL.Query()
	q.Set("query", query)
	req.URL.RawQuery = q.Encode()
	if a.user != "" {
		req.SetBasicAuth(a.user, a.password)
	}
	resp, err := chHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("clickhouse HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// Reset 清空日志表与 TopN 物化视图（恢复前调用）。
func (a *ChAdmin) Reset(ctx context.Context) error {
	if err := a.httpQuery(ctx, "TRUNCATE TABLE "+a.db+".logs", nil); err != nil {
		return err
	}
	// MV 的存储表名由 ClickHouse 生成，TRUNCATE MV 本身即可清空其内表。
	return a.httpQuery(ctx, "TRUNCATE TABLE "+a.db+".logs_topn_hourly", nil)
}

// RestoreMonth 将 Parquet 文件追加写入 logs 表。
func (a *ChAdmin) RestoreMonth(ctx context.Context, file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return a.httpQuery(ctx, "INSERT INTO "+a.db+".logs FORMAT Parquet", f)
}

// LogStats 在线日志规模与新鲜度（/metrics 与容量预警用）。
type LogStats struct {
	Rows      int64
	DiskBytes int64
	Parts     int64
	AgeSecs   int64 // 最新日志距今秒数；无数据为 -1
}

// LogStats 查询 system.parts 与 max(ts)，均为轻量元数据/主键查询。
func (a *ChAdmin) LogStats(ctx context.Context) (LogStats, error) {
	var out LogStats
	var latest time.Time
	if err := a.conn.QueryRow(ctx,
		`SELECT toInt64(coalesce(sum(rows),0)), toInt64(coalesce(sum(bytes_on_disk),0)), toInt64(count())
		   FROM system.parts WHERE database=? AND table='logs' AND active`, a.db).
		Scan(&out.Rows, &out.DiskBytes, &out.Parts); err != nil {
		return out, err
	}
	if err := a.conn.QueryRow(ctx, `SELECT max(ts) FROM `+a.db+`.logs`).Scan(&latest); err != nil {
		return out, err
	}
	if latest.IsZero() {
		out.AgeSecs = -1
	} else {
		out.AgeSecs = int64(time.Since(latest).Seconds())
		if out.AgeSecs < 0 {
			out.AgeSecs = 0
		}
	}
	return out, nil
}
