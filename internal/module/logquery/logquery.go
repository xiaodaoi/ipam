// Package logquery 日志中心检索域（M4-002，FR-E-04）。
//
// 职责边界：域类型 + 仓储接口（Store）+ 组织展开接口（OrgExpander）。
// Store 双实现：ClickHouse（生产/compose）与内存（PoC/单测），游标语义一致。
package logquery

import (
	"context"
	"time"
)

// LogRow 日志行（与 CH logs 宽表字段一一对应，§6；client_mac 为 12 位小写 hex 归一化键）。
type LogRow struct {
	TS        time.Time
	Type      string // dhcp | dns
	Severity  string
	ClientMAC string
	ClientIP  string
	SIP       string
	Domain    string
	Rcode     string
	Action    string
	Category  string
	AnswerIP  string
	Detail    string
}

// LogFilter /logs 组合过滤条件（游标分页语义见 Cursor）。
type LogFilter struct {
	From     time.Time // 必填；窗口上限 31 天由 service 校验
	To       time.Time // 零值=当前时间
	Type     string    // dhcp|dns，空=不过滤
	MAC      string    // 12-hex 或冒号分隔，service 归一化后传入
	IP       string    // 精确地址或 CIDR（client_ip/sip 任一命中）
	Domain   string    // 子串（不区分大小写）
	Action   string
	AnswerIP string // 应答 IP 过滤（toString(answer_ip) 子串）
	OrgID    string // 组织过滤（service 展开为 OrgScope 后传给 Store）
	Cursor   string
	PageSize int
}

// TopQuery /logs/top 参数（TopN 域名或客户端 MAC）。
type TopQuery struct {
	From   time.Time
	To     time.Time
	Type   string
	IP     string
	Action string
	OrgID  string
	By     string // domain | client
	Limit  int
}

// QpsQuery /logs/qps 时序参数。
type QpsQuery struct {
	From        time.Time
	To          time.Time
	Type        string
	Action      string
	OrgID       string
	IntervalSec int
}

// TopEntry TopN 条目。
type TopEntry struct {
	Key   string
	Count int
}

// QpsPoint 时序点（分桶起点 + 事件数）。
type QpsPoint struct {
	TS    time.Time
	Count int
}

// Page 分页结果。
type Page struct {
	Items      []LogRow
	NextCursor string // 空=已到尾页
	Total      int
}

// OrgScope 组织展开结果（§13.4 关联链：子树 CIDR 集合 ∪ 组内资产 MAC）。
type OrgScope struct {
	CIDRs []string // v4/v6 CIDR 文本
	MACs  []string // 12-hex 归一化
}

// OrgExpander 组织→CIDR∪MAC 展开回调（控制面 PG 侧实现，Store 不感知 PG）。
type OrgExpander interface {
	Expand(ctx context.Context, orgID string) (OrgScope, error)
}

// Store 日志仓储接口。实现方保证：过滤全语义一致、分页游标确定、Total 为同条件 count。
type Store interface {
	Query(ctx context.Context, f LogFilter, scope OrgScope) (Page, error)
	Top(ctx context.Context, q TopQuery, scope OrgScope) ([]TopEntry, int, error)
	Qps(ctx context.Context, q QpsQuery, scope OrgScope) ([]QpsPoint, error)

	// DistinctClientIP 去重客户端 IP 计数（f.Type/IP/时间窗/组织 scope 生效；
	// rng 非空时额外要求 client_ip 落 [Lo,Hi] 闭区间——池利用率口径）。
	DistinctClientIP(ctx context.Context, f LogFilter, scope OrgScope, rng *AddrRange) (int64, error)
	// HourlyActive 逐小时 distinct client_ip（DHCP 事件，[from,to]）。
	HourlyActive(ctx context.Context, from, to time.Time) ([]QpsPoint, error)
	// MacActivity 各 MAC 活动窗口 min/max（[from,to]，MAC 非空行）。
	MacActivity(ctx context.Context, from, to time.Time) (map[string][2]time.Time, error)
}

// AddrRange 地址闭区间（仪表盘池利用率入参；两端以 ::ffff 映射形态比较）。
type AddrRange struct {
	Lo, Hi string // 文本形态（v4/v6），内部映射后比较
}
