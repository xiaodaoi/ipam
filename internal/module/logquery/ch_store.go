package logquery

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	chdriver "github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// ChConfig 连接配置（env：IPAM_CH_ADDR/IPAM_CH_DB/IPAM_CH_USER/IPAM_CH_PASSWORD）。
type ChConfig struct {
	Addr     string // host:9000（原生协议）
	DB       string
	User     string
	Password string
}

// ChStore ClickHouse 实现（§6 查询网关）。
// 游标与 MemStore 同语义：(ts DESC, client_mac DESC, domain DESC) 全降序
// ⇒ 单一元组比较 (ts, client_mac, domain) < (?, ?, ?)，无 OR 分解。
type ChStore struct {
	conn chdriver.Conn
	db   string

	countMu    sync.Mutex
	countCache map[string]countEntry
}

const (
	countCacheTTL = 15 * time.Second
	countCacheMax = 256
)

type countEntry struct {
	n       int
	expires time.Time
}

// OpenChStore 建立连接（懒连接；max_execution_time=3s 硬顶即性能预算 §8）。
func OpenChStore(cfg ChConfig) (*ChStore, error) {
	opts := &clickhouse.Options{
		Addr: []string{cfg.Addr},
		Auth: clickhouse.Auth{Database: cfg.DB, Username: cfg.User, Password: cfg.Password},
		Settings: clickhouse.Settings{
			"max_execution_time": 3,
			// 资源护栏：单查询内存上限 + 聚合/排序落盘阈值，避免大时间窗/高基数
			// 查询把 ClickHouse 内存打满（容器总内存 7GB 级别）。
			"max_memory_usage":                   1_000_000_000,
			"max_bytes_before_external_group_by": 256_000_000,
			"max_bytes_before_external_sort":     256_000_000,
		},
		DialTimeout:  5 * time.Second,
		MaxOpenConns: 5,
		MaxIdleConns: 2,
	}
	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, err
	}
	return &ChStore{conn: conn, db: cfg.DB, countCache: map[string]countEntry{}}, nil
}

func (s *ChStore) table() string { return s.db + ".logs" }

const logCols = `ts, type, severity, client_mac,
  coalesce(toString(client_ip),''), coalesce(toString(sip),''),
  domain, rcode, action, category, coalesce(toString(answer_ip),''), detail`

// cond 三端点共享的过滤核心（Query/Top/Qps 字段交集）。
type cond struct {
	From, To time.Time
	Type     string
	MAC      string
	IP       string
	SIP      string
	Domain   string
	Action   string
	AnswerIP string
	Keyword  string
	Scope    OrgScope
}

// buildWhere 组合 WHERE 与绑定参数；各分支顺序固定便于单测断言。
func buildWhere(c cond) (string, []any) {
	var w []string
	var args []any

	w = append(w, "ts >= ? AND ts <= ?")
	if c.To.IsZero() {
		c.To = time.Now().UTC()
	}
	args = append(args, c.From.UTC(), c.To.UTC())

	if c.Type != "" {
		w = append(w, "type = ?")
		args = append(args, c.Type)
	}
	if c.MAC != "" {
		// 子串（模糊）匹配：归一化后 client_mac 为 12-hex 小写，兼容输入部分 MAC
		w = append(w, "positionCaseInsensitive(client_mac, ?) > 0")
		args = append(args, NormalizeMAC(c.MAC))
	}
	if c.IP != "" {
		w = append(w, "("+ipRangeClause(filterToRanges(c.IP), "client_ip", "sip")+")")
		args = appendRangeArgs(args, filterToRanges(c.IP), "client_ip", "sip")
	}
	if c.SIP != "" {
		// 来源 IP 子串（模糊）匹配：toString(sip) 为 IPv6 文本（IPv4 落成 ::ffff:a.b.c.d），兼容两种形态
		w = append(w, "positionCaseInsensitive(toString(sip), ?) > 0")
		args = append(args, c.SIP)
	}
	if c.Domain != "" {
		w = append(w, "positionCaseInsensitive(domain, ?) > 0")
		args = append(args, c.Domain)
	}
	if c.Action != "" {
		w = append(w, "action = ?")
		args = append(args, c.Action)
	}
	if c.AnswerIP != "" {
		// toString(answer_ip) 为 IPv6 文本（IPv4 落成 ::ffff:a.b.c.d），子串匹配兼容两种形态
		w = append(w, "positionCaseInsensitive(toString(answer_ip), ?) > 0")
		args = append(args, c.AnswerIP)
	}
	if c.Keyword != "" {
		// 单框模糊：域名 / 客户端 MAC / 客户端 IP / 来源 IP / 应答 IP 任一子串命中
		cols := []string{"domain", "client_mac", "toString(client_ip)", "toString(sip)", "toString(answer_ip)"}
		parts := make([]string, 0, len(cols))
		for _, col := range cols {
			parts = append(parts, "positionCaseInsensitive("+col+", ?) > 0")
			args = append(args, c.Keyword)
		}
		w = append(w, "("+strings.Join(parts, " OR ")+")")
	}
	scopeSQL, scopeArgs := buildScopeWhere(c.Scope)
	if scopeSQL != "" {
		w = append(w, scopeSQL)
		args = append(args, scopeArgs...)
	}
	return strings.Join(w, " AND "), args
}

// buildScopeWhere 组织展开命中块：client_mac IN (macs) ∪ (client_ip|sip) BETWEEN 合并区间。
func buildScopeWhere(scope OrgScope) (string, []any) {
	var parts []string
	var args []any
	macs := dedupNonEmpty(scope.MACs)
	for _, m := range macs {
		args = append(args, NormalizeMAC(m))
	}
	if len(macs) > 0 {
		parts = append(parts, "client_mac IN ("+placeholders(len(macs))+")")
	}
	ranges := scopeRanges(dedupNonEmpty(scope.CIDRs))
	if len(ranges) > 0 {
		parts = append(parts, ipRangeClause(ranges, "client_ip", "sip"))
		args = appendRangeArgs(args, ranges, "client_ip", "sip")
	}
	if len(parts) == 0 {
		return "", nil
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

// ipRangeClause N 段合并区间的 BETWEEN 列表达式（多列 OR 形态；占位符数=len(rs)*2*len(cols)）。
func ipRangeClause(rs []ipRange, cols ...string) string {
	var per []string
	for range rs {
		var colParts []string
		for _, col := range cols {
			colParts = append(colParts, col+" BETWEEN ? AND ?")
		}
		per = append(per, "("+strings.Join(colParts, " OR ")+")")
	}
	return strings.Join(per, " OR ")
}

func appendRangeArgs(args []any, rs []ipRange, cols ...string) []any {
	for _, r := range rs {
		for range cols {
			args = append(args, r.Lo, r.Hi)
		}
	}
	return args
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

func dedupNonEmpty(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// Query 组合过滤 + 元组游标分页 + 同条件精确 total。
func (s *ChStore) Query(ctx context.Context, f LogFilter, scope OrgScope) (Page, error) {
	where, args := buildWhere(cond{
		From: f.From, To: f.To, Type: f.Type, MAC: f.MAC, IP: f.IP, SIP: f.SIP,
		Domain: f.Domain, Action: f.Action, AnswerIP: f.AnswerIP, Keyword: f.Keyword, Scope: scope,
	})

	countSQL := "SELECT count() FROM " + s.table() + " WHERE " + where
	ck := countCacheKey(countSQL, args)
	total, ok := s.cachedCount(ck)
	if !ok {
		var n uint64
		if err := s.conn.QueryRow(ctx, countSQL, args...).Scan(&n); err != nil {
			return Page{}, err
		}
		total = int(n)
		s.storeCount(ck, total)
	}

	pageSize := f.PageSize
	if pageSize <= 0 {
		pageSize = DefaultPage
	}
	// 参数严格按 SQL 占位符顺序装配：where… → LIMIT ? → OFFSET ?
	// SQL 是 `... LIMIT ? OFFSET ?`，故 args 必须 [where…, limit, offset]——limit 在前、offset 在后！
	qargs := append([]any{}, args...)
	cursorCond := ""
	offsetClause := ""
	limit := pageSize
	if f.Cursor != "" {
		cts, cmac, cdomain := ParseCursor(f.Cursor)
		cursorCond = " AND (ts, client_mac, domain) < (?, ?, ?)"
		qargs = append(qargs, cts.UTC(), cmac, cdomain)
		limit = pageSize + 1 // 游标模式多取一条算 nextCursor
		qargs = append(qargs, limit)
	} else if f.Page > 1 {
		offsetClause = " OFFSET ?"
		qargs = append(qargs, limit, (f.Page-1)*pageSize)
	} else {
		qargs = append(qargs, limit)
	}
	sql := "SELECT " + logCols + " FROM " + s.table() +
		" WHERE " + where + cursorCond +
		" ORDER BY ts DESC, client_mac DESC, domain DESC LIMIT ?" + offsetClause

	rows, err := s.conn.Query(ctx, sql, qargs...)
	if err != nil {
		return Page{}, err
	}
	defer func() { _ = rows.Close() }()

	items := make([]LogRow, 0, pageSize)
	for rows.Next() {
		r, serr := scanLogRow(rows)
		if serr != nil {
			return Page{}, serr
		}
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}

	// offset 模式无需游标（页码翻页）；游标模式保留 nextCursor
	next := ""
	if f.Cursor != "" && len(items) > pageSize {
		last := items[pageSize-1]
		next = EncodeCursor(last.TS, last.ClientMAC, last.Domain)
		items = items[:pageSize]
	}
	return Page{Items: items, NextCursor: next, Total: total}, nil
}

// countCacheKey 基于 SQL + 绑定参数生成稳定键（时间转 UnixNano，避免 monotonic 差异）。
func countCacheKey(sql string, args []any) string {
	var b strings.Builder
	b.WriteString(sql)
	for _, a := range args {
		if t, ok := a.(time.Time); ok {
			fmt.Fprintf(&b, "|t%d", t.UnixNano())
		} else {
			fmt.Fprintf(&b, "|%v", a)
		}
	}
	sum := sha256.Sum256([]byte(b.String()))
	return string(sum[:])
}

func (s *ChStore) cachedCount(key string) (int, bool) {
	s.countMu.Lock()
	defer s.countMu.Unlock()
	e, ok := s.countCache[key]
	if !ok || time.Now().After(e.expires) {
		delete(s.countCache, key)
		return 0, false
	}
	return e.n, true
}

func (s *ChStore) storeCount(key string, n int) {
	s.countMu.Lock()
	defer s.countMu.Unlock()
	if len(s.countCache) >= countCacheMax {
		now := time.Now()
		for k, e := range s.countCache {
			if now.After(e.expires) {
				delete(s.countCache, k)
			}
		}
		if len(s.countCache) >= countCacheMax {
			for k := range s.countCache {
				delete(s.countCache, k)
				break
			}
		}
	}
	s.countCache[key] = countEntry{n: n, expires: time.Now().Add(countCacheTTL)}
}

// Top 原表聚合（同窗内毫秒~数百毫秒级，满足 ≤3s 预算；
// 物化视图 logs_topn_hourly 已预置，量级上升后按 §6 触发条件切换）。
func (s *ChStore) Top(ctx context.Context, q TopQuery, scope OrgScope) ([]TopEntry, int, error) {
	keyCol := "domain"
	if q.By == "client" {
		keyCol = "client_mac"
	}
	where, args := buildWhere(cond{
		From: q.From, To: q.To, Type: q.Type, IP: q.IP,
		Action: q.Action, Scope: scope,
	})
	where += " AND " + keyCol + " != ''"

	limit := q.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	sql := "SELECT " + keyCol + ", count() AS cnt FROM " + s.table() +
		" WHERE " + where + " GROUP BY " + keyCol +
		" ORDER BY cnt DESC, " + keyCol + " ASC LIMIT ?"
	qargs := append(append([]any{}, args...), limit)

	rows, err := s.conn.Query(ctx, sql, qargs...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	entries := make([]TopEntry, 0, limit)
	for rows.Next() {
		var k string
		var cnt uint64
		if err := rows.Scan(&k, &cnt); err != nil {
			return nil, 0, err
		}
		entries = append(entries, TopEntry{Key: k, Count: int(cnt)})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	var total uint64
	countSQL := "SELECT count() FROM " + s.table() + " WHERE " + where
	if err := s.conn.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	return entries, int(total), nil
}

// Qps toStartOfInterval 分桶计数（intervalSec 可变 ⇒ 不走 MV，§6 评审结论）。
func (s *ChStore) Qps(ctx context.Context, q QpsQuery, scope OrgScope) ([]QpsPoint, error) {
	interval := q.IntervalSec
	if interval < 1 || interval > 86400 {
		interval = DefaultBucket
	}
	where, args := buildWhere(cond{
		From: q.From, To: q.To, Type: q.Type, Action: q.Action, Scope: scope,
	})
	sql := "SELECT toStartOfInterval(ts, INTERVAL " + strconv.Itoa(interval) + " SECOND) AS bucket, count() AS cnt" +
		" FROM " + s.table() + " WHERE " + where +
		" GROUP BY bucket ORDER BY bucket ASC"
	rows, err := s.conn.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	points := make([]QpsPoint, 0, 128)
	for rows.Next() {
		var p QpsPoint
		var c uint64
		if err := rows.Scan(&p.TS, &c); err != nil {
			return nil, err
		}
		p.Count = int(c)
		points = append(points, p)
	}
	return points, rows.Err()
}

func scanLogRow(rows chdriver.Rows) (LogRow, error) {
	var (
		r                                                 LogRow
		sev, mac, clientIP, sip, dom, rc, act, cg, ai, dt string
	)
	if err := rows.Scan(&r.TS, &r.Type, &sev, &mac, &clientIP, &sip, &dom, &rc, &act, &cg, &ai, &dt); err != nil {
		return LogRow{}, err
	}
	r.Severity, r.ClientMAC = sev, mac
	r.ClientIP, r.SIP, r.AnswerIP = formatAddr(clientIP), formatAddr(sip), formatAddr(ai)
	r.Domain, r.Rcode, r.Action, r.Category, r.Detail = dom, rc, act, cg, dt
	return r, nil
}

// formatAddr 将 CH IPv6 文本归一化展示（::ffff:a.b.c.d → a.b.c.d）。
func formatAddr(s string) string {
	if s == "" {
		return ""
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return s
	}
	return a.Unmap().String()
}

// DistinctClientIP uniqExact 聚合（rng 非空时叠加 [Lo,Hi] BETWEEN）。
func (s *ChStore) DistinctClientIP(ctx context.Context, f LogFilter, scope OrgScope, rng *AddrRange) (int64, error) {
	where, args := buildWhere(cond{
		From: f.From, To: f.To, Type: f.Type, MAC: f.MAC, IP: f.IP,
		Domain: f.Domain, Action: f.Action, Scope: scope,
	})
	if rng != nil {
		where += " AND client_ip BETWEEN ? AND ?"
		lo, errL := netip.ParseAddr(rng.Lo)
		hi, errH := netip.ParseAddr(rng.Hi)
		if errL != nil || errH != nil {
			return 0, fmt.Errorf("addr range invalid: %s..%s", rng.Lo, rng.Hi)
		}
		args = append(args, mappedV6(lo), mappedV6(hi))
	}
	var n uint64
	err := s.conn.QueryRow(ctx,
		fmt.Sprintf(`SELECT uniqExact(coalesce(toString(client_ip),'')) FROM %s WHERE %s`, s.table(), where),
		args...).Scan(&n)
	return int64(n), err
}

// HourlyActive 逐小时 distinct client_ip（type=dhcp）。
func (s *ChStore) HourlyActive(ctx context.Context, from, to time.Time) ([]QpsPoint, error) {
	if to.IsZero() {
		to = time.Now().UTC()
	}
	rows, err := s.conn.Query(ctx,
		fmt.Sprintf(`SELECT toStartOfHour(ts), uniqExact(coalesce(toString(client_ip),''))
			FROM %s WHERE type='dhcp' AND ts >= ? AND ts <= ? GROUP BY 1 ORDER BY 1`, s.table()),
		from.UTC(), to.UTC())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []QpsPoint{}
	for rows.Next() {
		var p QpsPoint
		var c uint64
		if err := rows.Scan(&p.TS, &c); err != nil {
			return nil, err
		}
		p.Count = int(c)
		out = append(out, p)
	}
	return out, rows.Err()
}

// MacActivity 各 MAC min/max 活动窗口。
func (s *ChStore) MacActivity(ctx context.Context, from, to time.Time) (map[string][2]time.Time, error) {
	if to.IsZero() {
		to = time.Now().UTC()
	}
	rows, err := s.conn.Query(ctx,
		fmt.Sprintf(`SELECT client_mac, min(ts), max(ts) FROM %s
			WHERE type='dhcp' AND client_mac != '' AND ts >= ? AND ts <= ? GROUP BY client_mac`, s.table()),
		from.UTC(), to.UTC())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string][2]time.Time{}
	for rows.Next() {
		var mac string
		var mn, mx time.Time
		if err := rows.Scan(&mac, &mn, &mx); err != nil {
			return nil, err
		}
		out[mac] = [2]time.Time{mn, mx}
	}
	return out, rows.Err()
}

// Ping 健康灯探测（control-plane 探活用）。
func (s *ChStore) Ping(ctx context.Context) error { return s.conn.Ping(ctx) }

// Ping 内存实现的探活桩（存储层可达即 up）。
func (m *MemStore) Ping(context.Context) error { return nil }
