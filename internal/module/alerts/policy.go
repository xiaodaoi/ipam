package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// 通知模式（与 OpenAPI AlertNotifyMode 对齐）。
const (
	NotifyModeNotify = "notify"
	NotifyModeDigest = "digest"
	NotifyModeSilent = "silent"
	NotifyModeOff    = "off"
)

// 策略作用域。
const (
	ScopeGlobal      = "global"
	ScopeIntelSource = "intel_source"
	ScopeBlocklist   = "blocklist"
	ScopeKind        = "kind"
	ScopeComponent   = "component"
	ScopeSource      = "source"
)

// 报警分类。
const (
	ClassSecurityThreat = "security_threat"
	ClassPolicyBlock    = "policy_block"
	ClassSystemHealth   = "system_health"
	ClassConfigHealth   = "config_health"
)

// 聚合维度。
const (
	GroupByDomain = "domain"
	GroupByClient = "client"
	GroupBySource = "source"
	GroupByNone   = "none"
)

// Policy 一条告警策略。
type Policy struct {
	ID              string
	Name            string
	ScopeType       string
	ScopeKey        string
	Priority        int
	Severity        string
	NotifyMode      string
	AlertClass      string
	GroupBy         string
	MaxPerHour      int
	RepeatAfterSec  int
	ResolveAfterSec int
	Enabled         bool
	CreatedBy       string
	UpdatedBy       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// PolicyInput 策略解析输入。
type PolicyInput struct {
	Source       string
	Kind         string
	Component    string
	IntelSources []string
	ClientIP     string
	Domain       string
	Severity     string
}

// EffectivePolicy 解析后的有效策略（含抑制/静音结果）。
type EffectivePolicy struct {
	Policy
	MatchedBy    string
	Notify       bool
	SuppressedBy string
	SilenceID    string
	Capped       bool
	Final        string
}

// Decision 决策链（写入 alert.decision，供“为什么没通知”解释）。
type Decision struct {
	PolicyID     string `json:"policyId,omitempty"`
	PolicyName   string `json:"policyName,omitempty"`
	MatchedBy    string `json:"matchedBy,omitempty"`
	Severity     string `json:"severity,omitempty"`
	NotifyMode   string `json:"notifyMode,omitempty"`
	GroupBy      string `json:"groupBy,omitempty"`
	Notify       bool   `json:"notify"`
	SuppressedBy string `json:"suppressedBy,omitempty"`
	Capped       bool   `json:"capped,omitempty"`
	Final        string `json:"final,omitempty"`
}

// SilenceMatchers 静音匹配条件；空字段表示不限制。
type SilenceMatchers struct {
	Source      string `json:"source,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Component   string `json:"component,omitempty"`
	IntelSource string `json:"intelSource,omitempty"`
	Severity    string `json:"severity,omitempty"`
	ClientIP    string `json:"clientIp,omitempty"`
	Domain      string `json:"domain,omitempty"`
}

// Silence 静音窗口。
type Silence struct {
	ID        string
	Matchers  SilenceMatchers
	StartsAt  time.Time
	EndsAt    time.Time
	Reason    string
	CreatedBy string
	CreatedAt time.Time
}

// ActiveAt 判断静音是否在指定时刻生效。
func (s Silence) ActiveAt(now time.Time) bool {
	return !now.Before(s.StartsAt) && now.Before(s.EndsAt)
}

// Matches 判断报警输入是否命中静音条件。
func (s Silence) Matches(in PolicyInput, now time.Time) bool {
	if !s.ActiveAt(now) {
		return false
	}
	m := s.Matchers
	if m.Source != "" && m.Source != in.Source {
		return false
	}
	if m.Kind != "" && m.Kind != in.Kind {
		return false
	}
	if m.Component != "" && m.Component != in.Component {
		return false
	}
	if m.Severity != "" && m.Severity != in.Severity {
		return false
	}
	if m.ClientIP != "" && m.ClientIP != in.ClientIP {
		return false
	}
	joined := strings.ToLower(strings.Join(in.IntelSources, ","))
	if m.IntelSource != "" && !strings.Contains(joined, strings.ToLower(m.IntelSource)) {
		return false
	}
	if m.Domain != "" && !strings.Contains(strings.ToLower(in.Domain), strings.ToLower(m.Domain)) {
		return false
	}
	return true
}

func (m SilenceMatchers) Empty() bool {
	return m.Source == "" && m.Kind == "" && m.Component == "" && m.IntelSource == "" &&
		m.Severity == "" && m.ClientIP == "" && m.Domain == ""
}

func severityRank(s string) int {
	switch s {
	case SeverityCritical:
		return 3
	case SeverityWarning:
		return 2
	case SeverityInfo:
		return 1
	default:
		return 0
	}
}

func isSystemHealthKind(kind string) bool {
	switch kind {
	case KindDBDown, KindLogStoreDown, KindDHCPDown, KindDNSDown, KindDiskPressure, KindLogExportDegraded:
		return true
	}
	return false
}

func inferClass(kind string, intelKeys []string) string {
	switch {
	case isSystemHealthKind(kind):
		return ClassSystemHealth
	case kind == KindIntelSyncFailed || kind == KindUpstreamProbeFailed:
		return ClassConfigHealth
	}
	joined := strings.Join(intelKeys, ",")
	if strings.Contains(joined, "stevenblack") || strings.Contains(joined, "spamhaus") {
		return ClassPolicyBlock
	}
	if joined != "" {
		return ClassSecurityThreat
	}
	return ClassPolicyBlock
}

// DefaultPolicies 内置默认策略（首次启动落库；不覆盖用户修改）。
//
// 默认姿态：broad 聚合源归档不打扰，hot 威胁源立即通知，系统健康 critical 不可关闭。
func DefaultPolicies() []Policy {
	return []Policy{
		{
			Name: "全局兜底", ScopeType: ScopeGlobal, ScopeKey: "", Priority: 1000,
			Severity: SeverityWarning, NotifyMode: NotifyModeNotify, AlertClass: ClassPolicyBlock,
			GroupBy: GroupByDomain, MaxPerHour: 30, RepeatAfterSec: 86400, ResolveAfterSec: 86400, Enabled: true,
		},
		{
			Name: "StevenBlack 聚合归档", ScopeType: ScopeIntelSource, ScopeKey: "stevenblack-hosts",
			Priority: 100, Severity: SeverityInfo, NotifyMode: NotifyModeDigest, AlertClass: ClassPolicyBlock,
			GroupBy: GroupBySource, MaxPerHour: 5, RepeatAfterSec: 86400, ResolveAfterSec: 86400, Enabled: true,
		},
		{
			Name: "Spamhaus DBL 摘要", ScopeType: ScopeIntelSource, ScopeKey: "spamhaus-dbl",
			Priority: 100, Severity: SeverityWarning, NotifyMode: NotifyModeDigest, AlertClass: ClassSecurityThreat,
			GroupBy: GroupByDomain, MaxPerHour: 10, RepeatAfterSec: 86400, ResolveAfterSec: 86400, Enabled: true,
		},
		{
			Name: "URLhaus 威胁通知", ScopeType: ScopeIntelSource, ScopeKey: "abusech-urlhaus",
			Priority: 100, Severity: SeverityCritical, NotifyMode: NotifyModeNotify, AlertClass: ClassSecurityThreat,
			GroupBy: GroupByDomain, MaxPerHour: 30, RepeatAfterSec: 86400, ResolveAfterSec: 86400, Enabled: true,
		},
		{
			Name: "ThreatFox 威胁通知", ScopeType: ScopeIntelSource, ScopeKey: "abusech-threatfox",
			Priority: 100, Severity: SeverityCritical, NotifyMode: NotifyModeNotify, AlertClass: ClassSecurityThreat,
			GroupBy: GroupByDomain, MaxPerHour: 30, RepeatAfterSec: 86400, ResolveAfterSec: 86400, Enabled: true,
		},
		{
			Name: "OpenPhish 钓鱼通知", ScopeType: ScopeIntelSource, ScopeKey: "openphish-community",
			Priority: 100, Severity: SeverityWarning, NotifyMode: NotifyModeNotify, AlertClass: ClassSecurityThreat,
			GroupBy: GroupByDomain, MaxPerHour: 20, RepeatAfterSec: 86400, ResolveAfterSec: 86400, Enabled: true,
		},
		{
			Name: "核心服务不可用", ScopeType: ScopeKind, ScopeKey: KindDBDown,
			Priority: 10, Severity: SeverityCritical, NotifyMode: NotifyModeNotify, AlertClass: ClassSystemHealth,
			GroupBy: GroupByNone, MaxPerHour: 0, RepeatAfterSec: 900, ResolveAfterSec: 0, Enabled: true,
		},
		{
			Name: "日志存储不可用", ScopeType: ScopeKind, ScopeKey: KindLogStoreDown,
			Priority: 10, Severity: SeverityCritical, NotifyMode: NotifyModeNotify, AlertClass: ClassSystemHealth,
			GroupBy: GroupByNone, MaxPerHour: 0, RepeatAfterSec: 900, ResolveAfterSec: 0, Enabled: true,
		},
		{
			Name: "DHCP 不可用", ScopeType: ScopeKind, ScopeKey: KindDHCPDown,
			Priority: 10, Severity: SeverityCritical, NotifyMode: NotifyModeNotify, AlertClass: ClassSystemHealth,
			GroupBy: GroupByNone, MaxPerHour: 0, RepeatAfterSec: 900, ResolveAfterSec: 0, Enabled: true,
		},
		{
			Name: "DNS 不可用", ScopeType: ScopeKind, ScopeKey: KindDNSDown,
			Priority: 10, Severity: SeverityCritical, NotifyMode: NotifyModeNotify, AlertClass: ClassSystemHealth,
			GroupBy: GroupByNone, MaxPerHour: 0, RepeatAfterSec: 900, ResolveAfterSec: 0, Enabled: true,
		},
		{
			Name: "磁盘水位", ScopeType: ScopeKind, ScopeKey: KindDiskPressure,
			Priority: 20, Severity: SeverityWarning, NotifyMode: NotifyModeNotify, AlertClass: ClassSystemHealth,
			GroupBy: GroupByNone, MaxPerHour: 2, RepeatAfterSec: 3600, ResolveAfterSec: 0, Enabled: true,
		},
		{
			Name: "日志外发降级", ScopeType: ScopeKind, ScopeKey: KindLogExportDegraded,
			Priority: 20, Severity: SeverityWarning, NotifyMode: NotifyModeNotify, AlertClass: ClassSystemHealth,
			GroupBy: GroupByNone, MaxPerHour: 2, RepeatAfterSec: 3600, ResolveAfterSec: 0, Enabled: true,
		},
		{
			Name: "情报源同步失败", ScopeType: ScopeKind, ScopeKey: KindIntelSyncFailed,
			Priority: 20, Severity: SeverityWarning, NotifyMode: NotifyModeNotify, AlertClass: ClassConfigHealth,
			GroupBy: GroupBySource, MaxPerHour: 2, RepeatAfterSec: 3600, ResolveAfterSec: 0, Enabled: true,
		},
		{
			Name: "上游探活失败", ScopeType: ScopeKind, ScopeKey: KindUpstreamProbeFailed,
			Priority: 20, Severity: SeverityWarning, NotifyMode: NotifyModeNotify, AlertClass: ClassConfigHealth,
			GroupBy: GroupByNone, MaxPerHour: 2, RepeatAfterSec: 3600, ResolveAfterSec: 0, Enabled: true,
		},
	}
}

func defaultPolicy(scope, scopeKey string) Policy {
	for _, p := range DefaultPolicies() {
		if p.ScopeType == scope && p.ScopeKey == scopeKey {
			return p
		}
	}
	for _, p := range DefaultPolicies() {
		if p.ScopeType == ScopeGlobal {
			return p
		}
	}
	return Policy{Severity: SeverityWarning, NotifyMode: NotifyModeNotify, AlertClass: ClassPolicyBlock,
		GroupBy: GroupByDomain, RepeatAfterSec: 86400, ResolveAfterSec: 86400, Enabled: true}
}

func policySpecificity(p Policy) int {
	switch p.ScopeType {
	case ScopeIntelSource:
		return 5
	case ScopeBlocklist:
		return 4
	case ScopeKind:
		return 3
	case ScopeComponent:
		return 2
	case ScopeSource:
		return 1
	default:
		return 0
	}
}

func policyMatches(p Policy, in PolicyInput) bool {
	if !p.Enabled {
		return false
	}
	switch p.ScopeType {
	case ScopeGlobal:
		return true
	case ScopeSource:
		return p.ScopeKey == in.Source
	case ScopeComponent:
		return p.ScopeKey == in.Component
	case ScopeKind:
		return p.ScopeKey == in.Kind
	case ScopeIntelSource:
		for _, k := range in.IntelSources {
			if k == p.ScopeKey {
				return true
			}
		}
		return false
	case ScopeBlocklist:
		// 预留：自定义名单策略按 key 匹配（当前事件未携带 blocklist key）。
		return false
	}
	return false
}

// adminSnapshot 读取策略/静音缓存（30s TTL；无仓储时返回内置默认）。
func (s *Service) adminSnapshot(ctx context.Context) ([]Policy, []Silence) {
	if s.admin == nil {
		return DefaultPolicies(), nil
	}
	now := s.now()
	s.cacheMu.RLock()
	if !s.cacheAt.IsZero() && now.Sub(s.cacheAt) < 30*time.Second {
		ps, sl := s.cachePolicies, s.cacheSilences
		s.cacheMu.RUnlock()
		return ps, sl
	}
	s.cacheMu.RUnlock()

	policies, err := s.admin.ListPolicies(ctx)
	if err != nil {
		policies = nil
	}
	silences, err := s.admin.ListSilences(ctx)
	if err != nil {
		silences = nil
	}
	if len(policies) == 0 {
		policies = DefaultPolicies()
	}
	s.cacheMu.Lock()
	s.cachePolicies, s.cacheSilences, s.cacheAt = policies, silences, now
	s.cacheMu.Unlock()
	return policies, silences
}

// ResolvePolicy 解析有效策略：作用域优先、同级优先级、多源取最高 severity 且任一 notify 则通知。
func (s *Service) ResolvePolicy(ctx context.Context, in PolicyInput) EffectivePolicy {
	policies, silences := s.adminSnapshot(ctx)

	matched := make([]Policy, 0, 4)
	for _, p := range policies {
		if policyMatches(p, in) {
			matched = append(matched, p)
		}
	}
	// global 只是兜底：存在更具体的作用域时不再参与“任一 notify/最高级别”合成，
	// 否则全局兜底会覆盖 broad 源的归档默认值。
	specific := make([]Policy, 0, len(matched))
	for _, p := range matched {
		if p.ScopeType != ScopeGlobal {
			specific = append(specific, p)
		}
	}
	if len(specific) > 0 {
		best := 0
		for _, p := range specific {
			if s := policySpecificity(p); s > best {
				best = s
			}
		}
		matched = make([]Policy, 0, len(specific))
		for _, p := range specific {
			if policySpecificity(p) == best {
				matched = append(matched, p)
			}
		}
	}
	if len(matched) == 0 {
		matched = append(matched, defaultPolicy(ScopeGlobal, ""))
	}
	sort.SliceStable(matched, func(i, j int) bool {
		si, sj := policySpecificity(matched[i]), policySpecificity(matched[j])
		if si != sj {
			return si > sj
		}
		return matched[i].Priority < matched[j].Priority
	})

	chosen := matched[0]
	maxSev := chosen.Severity
	anyNotify := false
	for _, p := range matched {
		if severityRank(p.Severity) > severityRank(maxSev) {
			maxSev = p.Severity
		}
		if p.NotifyMode == NotifyModeNotify {
			anyNotify = true
		}
	}
	mode := chosen.NotifyMode
	if anyNotify {
		mode = NotifyModeNotify
	}
	// 系统健康类不允许通过 off 静默（可 digest/silent 归档，但 critical 强制通知）。
	if in.Source == SourceSystem && isSystemHealthKind(in.Kind) && mode == NotifyModeOff {
		mode = NotifyModeNotify
	}
	// critical 安全/健康事件默认强制通知，避免误配导致漏报；静音仍可显式压制。
	if maxSev == SeverityCritical && (chosen.AlertClass == ClassSecurityThreat || chosen.AlertClass == ClassSystemHealth) {
		if mode == NotifyModeOff {
			mode = NotifyModeNotify
		}
	}

	eff := EffectivePolicy{
		Policy:    chosen,
		MatchedBy: chosen.ScopeType,
		Notify:    mode == NotifyModeNotify,
	}
	eff.Severity = maxSev
	eff.NotifyMode = mode
	eff.AlertClass = inferClass(in.Kind, in.IntelSources)
	if chosen.AlertClass != "" {
		eff.AlertClass = chosen.AlertClass
	}

	now := s.now()
	sin := in
	sin.Severity = maxSev
	for _, sil := range silences {
		if sil.Matches(sin, now) {
			eff.Notify = false
			eff.SuppressedBy = "静音：" + sil.Reason
			eff.SilenceID = sil.ID
			break
		}
	}
	if eff.Notify {
		eff.Final = NotifyModeNotify
	} else {
		switch mode {
		case NotifyModeDigest:
			eff.Final = NotifyModeDigest
		case NotifyModeSilent:
			eff.Final = NotifyModeSilent
		default:
			eff.Final = NotifyModeOff
		}
		if eff.SuppressedBy != "" {
			eff.Final = NotifyModeSilent
		}
	}
	return eff
}

// DecisionOf 构造写入报警的决策链 JSON。
func DecisionOf(e EffectivePolicy) string {
	d := Decision{
		PolicyID: e.ID, PolicyName: e.Name, MatchedBy: e.MatchedBy,
		Severity: e.Severity, NotifyMode: e.NotifyMode, GroupBy: e.GroupBy,
		Notify: e.Notify, SuppressedBy: e.SuppressedBy, Capped: e.Capped, Final: e.Final,
	}
	raw, _ := json.Marshal(d)
	return string(raw)
}

// ── 策略 CRUD ──

func normalizePolicy(p Policy) (Policy, error) {
	p.Name = strings.TrimSpace(p.Name)
	p.ScopeType = strings.TrimSpace(p.ScopeType)
	p.ScopeKey = strings.TrimSpace(p.ScopeKey)
	if p.Name == "" {
		return p, fmt.Errorf("%w: 策略名称不能为空", ErrBadRequest)
	}
	switch p.ScopeType {
	case ScopeGlobal:
		p.ScopeKey = ""
	case ScopeIntelSource, ScopeBlocklist, ScopeKind, ScopeComponent, ScopeSource:
		if p.ScopeKey == "" {
			return p, fmt.Errorf("%w: 作用域对象不能为空", ErrBadRequest)
		}
	default:
		return p, fmt.Errorf("%w: 作用域非法", ErrBadRequest)
	}
	if p.Priority <= 0 {
		p.Priority = 100
	}
	if p.Severity == "" {
		p.Severity = SeverityWarning
	}
	switch p.Severity {
	case SeverityCritical, SeverityWarning, SeverityInfo:
	default:
		return p, fmt.Errorf("%w: 级别非法", ErrBadRequest)
	}
	if p.NotifyMode == "" {
		p.NotifyMode = NotifyModeNotify
	}
	switch p.NotifyMode {
	case NotifyModeNotify, NotifyModeDigest, NotifyModeSilent, NotifyModeOff:
	default:
		return p, fmt.Errorf("%w: 通知模式非法", ErrBadRequest)
	}
	if p.AlertClass == "" {
		p.AlertClass = ClassPolicyBlock
	}
	switch p.AlertClass {
	case ClassSecurityThreat, ClassPolicyBlock, ClassSystemHealth, ClassConfigHealth:
	default:
		return p, fmt.Errorf("%w: 报警分类非法", ErrBadRequest)
	}
	if p.GroupBy == "" {
		p.GroupBy = GroupByDomain
	}
	switch p.GroupBy {
	case GroupByDomain, GroupByClient, GroupBySource, GroupByNone:
	default:
		return p, fmt.Errorf("%w: 聚合维度非法", ErrBadRequest)
	}
	if p.MaxPerHour < 0 || p.RepeatAfterSec < 0 || p.ResolveAfterSec < 0 {
		return p, fmt.Errorf("%w: 频率参数不能为负", ErrBadRequest)
	}
	return p, nil
}

// Policies 返回全部策略。
func (s *Service) Policies(ctx context.Context) ([]Policy, error) {
	if s.admin == nil {
		return DefaultPolicies(), nil
	}
	return s.admin.ListPolicies(ctx)
}

// GetPolicy 按 ID 查询。
func (s *Service) GetPolicy(ctx context.Context, id string) (Policy, bool, error) {
	if s.admin == nil {
		return Policy{}, false, nil
	}
	list, err := s.admin.ListPolicies(ctx)
	if err != nil {
		return Policy{}, false, err
	}
	for _, p := range list {
		if p.ID == id {
			return p, true, nil
		}
	}
	return Policy{}, false, nil
}

// CreatePolicy 新增策略。
func (s *Service) CreatePolicy(ctx context.Context, p Policy, actor string) (Policy, error) {
	p, err := normalizePolicy(p)
	if err != nil {
		return Policy{}, err
	}
	if s.admin == nil {
		return Policy{}, fmt.Errorf("%w: 策略存储不可用", ErrBadRequest)
	}
	p.CreatedBy, p.UpdatedBy = actor, actor
	out, err := s.admin.CreatePolicy(ctx, p)
	if err == nil {
		s.invalidatePolicyCache()
	}
	return out, err
}

// PolicyPatch 局部更新字段。
type PolicyPatch struct {
	Name            *string
	Priority        *int
	Severity        *string
	NotifyMode      *string
	AlertClass      *string
	GroupBy         *string
	MaxPerHour      *int
	RepeatAfterSec  *int
	ResolveAfterSec *int
	Enabled         *bool
}

// UpdatePolicy 局部更新策略。
func (s *Service) UpdatePolicy(ctx context.Context, id string, patch PolicyPatch, actor string) (Policy, error) {
	if s.admin == nil {
		return Policy{}, fmt.Errorf("%w: 策略存储不可用", ErrBadRequest)
	}
	list, err := s.admin.ListPolicies(ctx)
	if err != nil {
		return Policy{}, err
	}
	var cur Policy
	found := false
	for _, p := range list {
		if p.ID == id {
			cur, found = p, true
			break
		}
	}
	if !found {
		return Policy{}, ErrNotFound
	}
	if patch.Name != nil {
		cur.Name = *patch.Name
	}
	if patch.Priority != nil {
		cur.Priority = *patch.Priority
	}
	if patch.Severity != nil {
		cur.Severity = *patch.Severity
	}
	if patch.NotifyMode != nil {
		cur.NotifyMode = *patch.NotifyMode
	}
	if patch.AlertClass != nil {
		cur.AlertClass = *patch.AlertClass
	}
	if patch.GroupBy != nil {
		cur.GroupBy = *patch.GroupBy
	}
	if patch.MaxPerHour != nil {
		cur.MaxPerHour = *patch.MaxPerHour
	}
	if patch.RepeatAfterSec != nil {
		cur.RepeatAfterSec = *patch.RepeatAfterSec
	}
	if patch.ResolveAfterSec != nil {
		cur.ResolveAfterSec = *patch.ResolveAfterSec
	}
	if patch.Enabled != nil {
		cur.Enabled = *patch.Enabled
	}
	cur, err = normalizePolicy(cur)
	if err != nil {
		return Policy{}, err
	}
	cur.UpdatedBy = actor
	out, err := s.admin.UpdatePolicy(ctx, cur)
	if err == nil {
		s.invalidatePolicyCache()
	}
	return out, err
}

// DeletePolicy 删除策略；全局兜底不可删。
func (s *Service) DeletePolicy(ctx context.Context, id string) error {
	if s.admin == nil {
		return fmt.Errorf("%w: 策略存储不可用", ErrBadRequest)
	}
	p, ok, err := s.GetPolicy(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	if p.ScopeType == ScopeGlobal {
		return fmt.Errorf("%w: 全局兜底策略不可删除", ErrBadRequest)
	}
	err = s.admin.DeletePolicy(ctx, id)
	if err == nil {
		s.invalidatePolicyCache()
	}
	return err
}

// ── 静音 CRUD ──

// Silences 返回全部静音窗口。
func (s *Service) Silences(ctx context.Context) ([]Silence, error) {
	if s.admin == nil {
		return []Silence{}, nil
	}
	return s.admin.ListSilences(ctx)
}

// CreateSilence 新增静音窗口。
func (s *Service) CreateSilence(ctx context.Context, sil Silence, actor string) (Silence, error) {
	sil.Reason = strings.TrimSpace(sil.Reason)
	if sil.Reason == "" {
		return Silence{}, fmt.Errorf("%w: 静音理由不能为空", ErrBadRequest)
	}
	if sil.Matchers.Empty() {
		return Silence{}, fmt.Errorf("%w: 至少需要一个匹配条件", ErrBadRequest)
	}
	if sil.StartsAt.IsZero() {
		sil.StartsAt = s.now()
	}
	if sil.EndsAt.IsZero() || !sil.EndsAt.After(sil.StartsAt) {
		return Silence{}, fmt.Errorf("%w: 结束时间必须晚于开始时间", ErrBadRequest)
	}
	if s.admin == nil {
		return Silence{}, fmt.Errorf("%w: 静音存储不可用", ErrBadRequest)
	}
	sil.CreatedBy = actor
	out, err := s.admin.CreateSilence(ctx, sil)
	if err == nil {
		s.invalidatePolicyCache()
	}
	return out, err
}

// DeleteSilence 撤销静音。
func (s *Service) DeleteSilence(ctx context.Context, id string) error {
	if s.admin == nil {
		return fmt.Errorf("%w: 静音存储不可用", ErrBadRequest)
	}
	err := s.admin.DeleteSilence(ctx, id)
	if err == nil {
		s.invalidatePolicyCache()
	}
	return err
}

// AutoResolveDNS 按策略快照自动恢复长时间无命中的 DNS 报警。
func (s *Service) AutoResolveDNS(ctx context.Context) (int, error) {
	if s.admin == nil {
		return 0, nil
	}
	return s.admin.AutoResolveDNS(ctx, s.now())
}

// SimulationResult 策略模拟结果（不改变运行态）。
type SimulationResult struct {
	Recorded           int
	Notified           int
	Archived           int
	Suppressed         int
	CriticalNotified   int
	CriticalSuppressed int
	BySeverity         map[string]int
}

// SimulatePolicy 按候选策略回放最近窗口的报警档案，估算通知/归档/漏报量。
func (s *Service) SimulatePolicy(ctx context.Context, p Policy, from, to time.Time) (SimulationResult, error) {
	p, err := normalizePolicy(p)
	if err != nil {
		return SimulationResult{}, err
	}
	if to.IsZero() {
		to = s.now()
	}
	if from.IsZero() {
		from = to.Add(-7 * 24 * time.Hour)
	}
	res := SimulationResult{BySeverity: map[string]int{}}
	const pageSize = 200
	for page := 1; page <= 25; page++ {
		items, total, err := s.repo.List(ctx, Filter{From: from, To: to, Page: page, PageSize: pageSize})
		if err != nil {
			return SimulationResult{}, err
		}
		for _, a := range items {
			res.Recorded++
			sev, notify, suppressed := applyCandidate(p, a)
			res.BySeverity[sev]++
			if notify {
				res.Notified++
				if sev == SeverityCritical {
					res.CriticalNotified++
				}
			} else {
				res.Archived++
				if sev == SeverityCritical {
					res.CriticalSuppressed++
				}
			}
			if suppressed {
				res.Suppressed++
			}
		}
		if len(items) == 0 || page*pageSize >= total {
			break
		}
	}
	return res, nil
}

func applyCandidate(p Policy, a Alert) (severity string, notify bool, suppressed bool) {
	severity = a.Severity
	notify = a.Notify
	suppressed = a.SuppressedBy != ""
	if !policyMatchesAlert(p, a) {
		return severity, notify, suppressed
	}
	if p.Severity != "" {
		severity = p.Severity
	}
	switch p.NotifyMode {
	case NotifyModeNotify:
		notify = true
	case NotifyModeDigest, NotifyModeSilent, NotifyModeOff:
		notify = false
		suppressed = true
	}
	return severity, notify, suppressed
}

func policyMatchesAlert(p Policy, a Alert) bool {
	switch p.ScopeType {
	case ScopeGlobal:
		return true
	case ScopeSource:
		return p.ScopeKey == a.Source
	case ScopeKind:
		return p.ScopeKey == a.Kind
	case ScopeComponent:
		return p.ScopeKey == a.Component
	case ScopeIntelSource:
		for _, k := range splitKeys(a.IntelSource) {
			if k == p.ScopeKey {
				return true
			}
		}
		return false
	}
	return false
}
