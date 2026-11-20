package logquery

import (
	"context"
	"time"
)

// BlockedGroup 风险域名访问聚合（报警中心 M6-040）。
//
// 以「小时桶 + 客户端 IP + 域名」聚合：同一小时内的重复访问累加为一条报警，
// 跨小时自然拆分为不同记录；采集器重复回扫时按累计值合并，不会重复计数。
type BlockedGroup struct {
	Bucket    time.Time
	ClientIP  string
	Domain    string
	Category  string
	Count     int
	FirstSeen time.Time
	LastSeen  time.Time
}

// BlockedEvents 拉取 since 之后被拦截的 DNS 访问聚合。
//
// 只统计 type=dns 且 action=blocked 的记录；sip 为 DNS 查询源地址（客户端）。
// 额外排除含通配符的域名：RPZ 通配规则（如 *.corp.example）不是真实查询名，
// 历史上白名单 passthru 曾被误记为 blocked 并写入规则名，这里做兜底过滤。
// 单次返回上限 5000 组，超出按命中次数倒序截断，避免极端场景把控制面内存打满。
func (s *ChStore) BlockedEvents(ctx context.Context, since time.Time) ([]BlockedGroup, error) {
	sql := `SELECT toStartOfHour(ts) AS bucket,
	  coalesce(toString(sip),'') AS client_ip,
	  domain,
	  any(category) AS category,
	  count() AS n,
	  min(ts) AS first_seen,
	  max(ts) AS last_seen
	FROM ` + s.table() + `
	WHERE ts >= ? AND type='dns' AND action='blocked' AND domain != '' AND sip IS NOT NULL
	  AND position(domain, '*') = 0
	GROUP BY bucket, client_ip, domain
	ORDER BY n DESC
	LIMIT 5000`
	rows, err := s.conn.Query(ctx, sql, since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]BlockedGroup, 0, 128)
	for rows.Next() {
		var (
			g        BlockedGroup
			n        uint64
			category string
		)
		if err := rows.Scan(&g.Bucket, &g.ClientIP, &g.Domain, &category, &n, &g.FirstSeen, &g.LastSeen); err != nil {
			return nil, err
		}
		g.Category = category
		g.Count = int(n)
		g.ClientIP = formatAddr(g.ClientIP)
		out = append(out, g)
	}
	return out, rows.Err()
}
