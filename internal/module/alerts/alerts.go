// Package alerts 实现应用内报警中心（M6-040）。
//
// 报警是聚合结果而非日志副本：
//   - DNS 风险访问：从 ClickHouse logs 宽表按 (客户端 IP, 域名, 小时) 聚合 action=blocked；
//   - 系统故障：数据库/日志存储/DHCP/DNS 掉线、磁盘水位、日志外发降级、情报源失败、上游探活失败。
//
// 确认语义为全局确认（单机产品），未确认（open）数量驱动右上角徽标。
package alerts

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

// 报警来源。
const (
	SourceDNS    = "dns"
	SourceSystem = "system"
)

// 报警类型。
const (
	KindRiskURL             = "risk_url"
	KindDBDown              = "db_down"
	KindLogStoreDown        = "logstore_down"
	KindDHCPDown            = "dhcp_down"
	KindDNSDown             = "dns_down"
	KindDiskPressure        = "disk_pressure"
	KindLogExportDegraded   = "log_export_degraded"
	KindIntelSyncFailed     = "intel_sync_failed"
	KindUpstreamProbeFailed = "upstream_probe_failed"
)

// 报警级别。
const (
	SeverityCritical = "critical"
	SeverityWarning  = "warning"
	SeverityInfo     = "info"
)

// 报警状态。
const (
	StatusOpen     = "open"
	StatusAcked    = "acked"
	StatusResolved = "resolved"
)

const (
	DefaultPage     = 20
	MaxPageSize     = 200
	DefaultLookback = 7 * 24 * time.Hour
	MaxWindow       = 366 * 24 * time.Hour
	DefaultUnread   = 8
	MaxUnread       = 50
	Retention       = 90 * 24 * time.Hour
)

var (
	ErrBadWindow  = errors.New("时间窗非法（from 不能晚于 to，且不超过 366 天）")
	ErrBadRequest = errors.New("请求参数非法")
	ErrNotFound   = errors.New("记录不存在")
	ErrConflict   = errors.New("记录已存在")
)

// Alert 一条报警记录。
type Alert struct {
	ID              string
	DedupKey        string
	Fingerprint     string
	Source          string
	Kind            string
	Severity        string
	Status          string
	Title           string
	Message         string
	ClientIP        string
	ClientMAC       string
	Domain          string
	Component       string
	IntelSource     string
	IntelSourceName string
	AlertClass      string
	Notify          bool
	PolicyID        string
	GroupKey        string
	Count           int
	Detail          string
	WindowStart     time.Time
	NotifyCount     int
	LastNotifiedAt  *time.Time
	SuppressedBy    string
	SilenceID       string
	Decision        string // 决策链 JSON
	RepeatAfterSec  int
	ResolveAfterSec int
	FirstSeen       time.Time
	LastSeen        time.Time
	AckedAt         *time.Time
	AckedBy         string
	ResolvedAt      *time.Time
	CreatedAt       time.Time
	// JustNotified 仅用于采集器判断本轮是否新产生通知（不入库、不出 API）。
	JustNotified bool
}

// Filter 列表/统计过滤条件（时间按 last_seen）。
type Filter struct {
	From        time.Time
	To          time.Time
	Q           string
	Source      string
	Kind        string
	Severity    string
	Status      string
	IntelSource string // 情报来源（key 或显示名子串）
	AlertClass  string
	Notify      *bool // nil=全部；true=仅需关注；false=仅归档
	Page        int
	PageSize    int
	Bucket      string // 趋势桶：hour | day（Service 按窗口自动选择）
}

// Page 列表结果。
type Page struct {
	Items    []Alert
	Total    int
	Page     int
	PageSize int
}

// Aggregate 统计聚合项。
type Aggregate struct {
	Key   string
	Count int
}

// TrendPoint 趋势点。
type TrendPoint struct {
	TS    time.Time
	Count int
}

// SourceStat 按来源/组件统计（降噪总览用）。
type SourceStat struct {
	Key      string
	Recorded int
	Notified int
}

// Stats 报警统计。
type Stats struct {
	Total              int
	Open               int
	Acked              int
	Resolved           int
	Notified           int
	Archived           int
	Suppressed         int
	CriticalSuppressed int
	BySeverity         map[string]int
	ByKind             map[string]int
	ByIntelSource      map[string]int // 情报来源显示名 → 报警数（仅 DNS 风险访问）
	BySource           []SourceStat
	TopDomains         []Aggregate
	TopClients         []Aggregate
	Trend              []TrendPoint
	NotifiedTrend      []TrendPoint
	Bucket             string // hour | day
}

// Unread 报警栏数据。
type Unread struct {
	Count int
	Items []Alert
}

// Event 采集器上报的报警事件。
type Event struct {
	DedupKey        string
	Fingerprint     string
	Source          string
	Kind            string
	Severity        string
	Title           string
	Message         string
	ClientIP        string
	ClientMAC       string
	Domain          string
	Component       string
	IntelSource     string // 命中情报源的 key（多个用逗号分隔）
	IntelSourceName string // 命中情报源的显示名（历史留痕）
	AlertClass      string
	Notify          bool
	PolicyID        string
	GroupKey        string
	Count           int
	Detail          string
	WindowStart     time.Time
	NotifyCount     int
	LastNotifiedAt  time.Time
	SuppressedBy    string
	SilenceID       string
	Decision        string
	RepeatAfterSec  int
	ResolveAfterSec int
	MaxPerHour      int // 仅采集器限流用，不入库
	FirstSeen       time.Time
	LastSeen        time.Time
}

// Repo 报警持久化。
type Repo interface {
	List(ctx context.Context, f Filter) ([]Alert, int, error)
	Stats(ctx context.Context, f Filter) (Stats, error)
	Unread(ctx context.Context, limit int) (Unread, error)
	Ack(ctx context.Context, ids []string, actor string, at time.Time) (int, error)
	AckAll(ctx context.Context, actor string, at time.Time) (int, error)
	Upsert(ctx context.Context, ev Event) (Alert, error)
	Resolve(ctx context.Context, dedupKey string, at time.Time) (bool, error)
	// ResolveStaleSystem 把 managedKinds 中、不在 activeKeys 里的未恢复系统报警置为 resolved。
	ResolveStaleSystem(ctx context.Context, managedKinds, activeKeys []string, at time.Time) (int, error)
	Purge(ctx context.Context, before time.Time) (int, error)
}

// AdminRepo 告警治理持久化（策略/静音/通道/投递/自动恢复）。
// PgRepo 与 MemRepo 均实现；Service 构造时自动探测，测试无需额外装配。
type AdminRepo interface {
	ListPolicies(ctx context.Context) ([]Policy, error)
	CreatePolicy(ctx context.Context, p Policy) (Policy, error)
	UpdatePolicy(ctx context.Context, p Policy) (Policy, error)
	DeletePolicy(ctx context.Context, id string) error
	SeedPolicies(ctx context.Context, ps []Policy) error

	ListSilences(ctx context.Context) ([]Silence, error)
	CreateSilence(ctx context.Context, s Silence) (Silence, error)
	DeleteSilence(ctx context.Context, id string) error

	ListChannels(ctx context.Context) ([]Channel, error)
	GetChannel(ctx context.Context, id string) (Channel, bool, error)
	CreateChannel(ctx context.Context, ch Channel) (Channel, error)
	UpdateChannel(ctx context.Context, ch Channel) (Channel, error)
	DeleteChannel(ctx context.Context, id string) error

	CreateDelivery(ctx context.Context, d Delivery) error
	ListDeliveries(ctx context.Context, f DeliveryFilter) ([]Delivery, int, error)

	AutoResolveDNS(ctx context.Context, now time.Time) (int, error)
}

// Service 报警业务编排。
type Service struct {
	repo  Repo
	admin AdminRepo
	now   func() time.Time

	secretDir string

	cacheMu       sync.RWMutex
	cachePolicies []Policy
	cacheSilences []Silence
	cacheAt       time.Time
}

// NewService 构造。
func NewService(repo Repo) *Service {
	s := &Service{repo: repo, now: func() time.Time { return time.Now().UTC() }}
	if a, ok := repo.(AdminRepo); ok {
		s.admin = a
	}
	return s
}

// SetAdminRepo 注入告警治理仓储（无 PG 时可缺省，策略回落到内置默认）。
func (s *Service) SetAdminRepo(a AdminRepo) {
	if a != nil {
		s.admin = a
		s.invalidatePolicyCache()
	}
}

// SetSecretDir 设置通知通道密钥文件目录。
func (s *Service) SetSecretDir(dir string) {
	if dir != "" {
		s.secretDir = dir
	}
}

func (s *Service) invalidatePolicyCache() {
	s.cacheMu.Lock()
	s.cachePolicies, s.cacheSilences, s.cacheAt = nil, nil, time.Time{}
	s.cacheMu.Unlock()
}

// EnsurePolicies 首次落库默认策略（不覆盖用户修改）。
func (s *Service) EnsurePolicies(ctx context.Context) error {
	if s.admin == nil {
		return nil
	}
	return s.admin.SeedPolicies(ctx, DefaultPolicies())
}

// SetNow 注入时钟（单测）。
func (s *Service) SetNow(f func() time.Time) {
	if f != nil {
		s.now = f
	}
}

// List 查询报警。
func (s *Service) List(ctx context.Context, f Filter) (Page, error) {
	if err := s.normalize(&f); err != nil {
		return Page{}, err
	}
	items, total, err := s.repo.List(ctx, f)
	if err != nil {
		return Page{}, err
	}
	return Page{Items: items, Total: total, Page: f.Page, PageSize: f.PageSize}, nil
}

// Stats 报警统计。
func (s *Service) Stats(ctx context.Context, f Filter) (Stats, error) {
	if err := s.normalize(&f); err != nil {
		return Stats{}, err
	}
	f.Page, f.PageSize = 0, 0
	st, err := s.repo.Stats(ctx, f)
	if err != nil {
		return Stats{}, err
	}
	if st.BySeverity == nil {
		st.BySeverity = map[string]int{}
	}
	if st.ByKind == nil {
		st.ByKind = map[string]int{}
	}
	if st.ByIntelSource == nil {
		st.ByIntelSource = map[string]int{}
	}
	if st.BySource == nil {
		st.BySource = []SourceStat{}
	}
	if st.TopDomains == nil {
		st.TopDomains = []Aggregate{}
	}
	if st.TopClients == nil {
		st.TopClients = []Aggregate{}
	}
	if st.Trend == nil {
		st.Trend = []TrendPoint{}
	}
	if st.NotifiedTrend == nil {
		st.NotifiedTrend = []TrendPoint{}
	}
	st.Bucket = f.Bucket
	return st, nil
}

// Unread 报警栏最近未确认。
func (s *Service) Unread(ctx context.Context, limit int) (Unread, error) {
	if limit <= 0 {
		limit = DefaultUnread
	}
	if limit > MaxUnread {
		limit = MaxUnread
	}
	return s.repo.Unread(ctx, limit)
}

// Ack 确认单条或批量（幂等：仅 open → acked）。
func (s *Service) Ack(ctx context.Context, ids []string, actor string) (int, error) {
	clean := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			clean = append(clean, id)
		}
	}
	if len(clean) == 0 {
		return 0, ErrBadRequest
	}
	if actor == "" {
		actor = "system"
	}
	return s.repo.Ack(ctx, clean, actor, s.now())
}

// AckAll 确认全部未确认。
func (s *Service) AckAll(ctx context.Context, actor string) (int, error) {
	if actor == "" {
		actor = "system"
	}
	return s.repo.AckAll(ctx, actor, s.now())
}

// Report 上报一条报警（系统故障恢复由 ResolveStaleSystem 负责）。
func (s *Service) Report(ctx context.Context, ev Event) (Alert, error) {
	if ev.DedupKey == "" || ev.Kind == "" {
		return Alert{}, ErrBadRequest
	}
	if ev.Fingerprint == "" {
		ev.Fingerprint = ev.DedupKey
	}
	if ev.Severity == "" {
		ev.Severity = SeverityWarning
	}
	if ev.Source == "" {
		ev.Source = SourceSystem
	}
	if ev.LastSeen.IsZero() {
		ev.LastSeen = s.now()
	}
	if ev.FirstSeen.IsZero() {
		ev.FirstSeen = ev.LastSeen
	}
	if ev.WindowStart.IsZero() {
		ev.WindowStart = ev.LastSeen.UTC().Truncate(time.Hour)
	}
	if ev.Count <= 0 {
		ev.Count = 1
	}
	// 兼容直接调用（历史/单测）：未经过策略引擎的事件默认通知。
	if ev.Decision == "" && ev.AlertClass == "" && ev.PolicyID == "" && ev.SuppressedBy == "" {
		ev.Notify = true
		ev.AlertClass = inferClass(ev.Kind, splitKeys(ev.IntelSource))
		ev.Decision = `{"notify":true,"final":"notify","matchedBy":"direct"}`
	}
	if ev.RepeatAfterSec == 0 && ev.Source == SourceDNS {
		ev.RepeatAfterSec = 86400
	}
	if ev.ResolveAfterSec == 0 && ev.Source == SourceDNS {
		ev.ResolveAfterSec = 86400
	}
	return s.repo.Upsert(ctx, ev)
}

// Resolve 恢复单条系统报警。
func (s *Service) Resolve(ctx context.Context, dedupKey string) (bool, error) {
	return s.repo.Resolve(ctx, dedupKey, s.now())
}

// ResolveStaleSystem 批量恢复已不在当前故障快照中的系统报警。
func (s *Service) ResolveStaleSystem(ctx context.Context, managedKinds, activeKeys []string) (int, error) {
	return s.repo.ResolveStaleSystem(ctx, managedKinds, activeKeys, s.now())
}

// Purge 清理过期报警。
func (s *Service) Purge(ctx context.Context, before time.Time) (int, error) {
	return s.repo.Purge(ctx, before)
}

func (s *Service) normalize(f *Filter) error {
	now := s.now()
	if f.To.IsZero() {
		f.To = now
	}
	if f.From.IsZero() {
		f.From = f.To.Add(-DefaultLookback)
	}
	if f.From.After(f.To) || f.To.Sub(f.From) > MaxWindow {
		return ErrBadWindow
	}
	f.Q = strings.TrimSpace(f.Q)
	f.Source = strings.TrimSpace(f.Source)
	f.IntelSource = strings.TrimSpace(f.IntelSource)
	f.Kind = strings.TrimSpace(f.Kind)
	f.Severity = strings.TrimSpace(f.Severity)
	f.Status = strings.TrimSpace(f.Status)
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 {
		f.PageSize = DefaultPage
	}
	if f.PageSize > MaxPageSize {
		f.PageSize = MaxPageSize
	}
	if f.To.Sub(f.From) <= 48*time.Hour {
		f.Bucket = "hour"
	} else {
		f.Bucket = "day"
	}
	return nil
}

// sortAlerts 未确认优先、last_seen 倒序（内存实现与 SQL 语义一致）。
func sortAlerts(items []Alert) {
	rank := map[string]int{StatusOpen: 0, StatusAcked: 1, StatusResolved: 2}
	sort.SliceStable(items, func(i, j int) bool {
		ri, rj := rank[items[i].Status], rank[items[j].Status]
		if ri != rj {
			return ri < rj
		}
		if !items[i].LastSeen.Equal(items[j].LastSeen) {
			return items[i].LastSeen.After(items[j].LastSeen)
		}
		return items[i].ID < items[j].ID
	})
}

// matchText 内存实现的模糊搜索口径（与 PG ILIKE 对齐）。
func matchText(a Alert, q string) bool {
	if q == "" {
		return true
	}
	q = strings.ToLower(q)
	for _, v := range []string{a.Title, a.Message, a.Domain, a.ClientIP, a.ClientMAC,
		a.Component, a.IntelSource, a.IntelSourceName, a.AlertClass} {
		if strings.Contains(strings.ToLower(v), q) {
			return true
		}
	}
	return false
}

func matchFilter(a Alert, f Filter) bool {
	if !f.From.IsZero() && a.LastSeen.Before(f.From) {
		return false
	}
	if !f.To.IsZero() && a.LastSeen.After(f.To) {
		return false
	}
	if f.Source != "" && a.Source != f.Source {
		return false
	}
	if f.Kind != "" && a.Kind != f.Kind {
		return false
	}
	if f.Severity != "" && a.Severity != f.Severity {
		return false
	}
	if f.Status != "" && a.Status != f.Status {
		return false
	}
	if f.AlertClass != "" && a.AlertClass != f.AlertClass {
		return false
	}
	if f.Notify != nil && a.Notify != *f.Notify {
		return false
	}
	if f.IntelSource != "" &&
		!strings.Contains(strings.ToLower(a.IntelSource+" "+a.IntelSourceName), strings.ToLower(f.IntelSource)) {
		return false
	}
	return matchText(a, f.Q)
}
