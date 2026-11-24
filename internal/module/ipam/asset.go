package ipam

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/xiaodaoi/ipam/internal/module/coherence"
)

// Asset 资产登记（PG asset 表，身份键 = MAC 或 DUID，§13.4）。
// M6-046：IPv6-only 终端只有 DUID（coherence_binding.mac 存 DUID、ipv4=0.0.0.0），
// 原先强制 MAC 身份键会把这类终端整个丢掉，因此身份键允许退化为 DUID。
type Asset struct {
	MAC   string
	DUID  string
	OrgID string
	Owner string
	Dept  string
	Note  string
	Tags  []string
}

// Identity 身份键：优先归一化 MAC，回退 DUID。
func (a Asset) Identity() string {
	if a.MAC != "" {
		return a.MAC
	}
	return a.DUID
}

// DiscoveredTerminal 未登记终端（有租约/绑定但 asset 表无记录）。
type DiscoveredTerminal struct {
	Identity   string // 身份键（MAC 优先，回退 DUID）
	MAC        string
	DUID       string
	IPv4       string
	IPv6       string
	Hostname   string
	State      string
	SubnetID   string
	SubnetCIDR string
	OrgID      string
	Cltt       int64 // 租约最近开始时间（unix 秒）
	LastSeen   time.Time
}

// AssetQuery 资产列表查询（服务端分页 + 筛选，M6-048）。
// OrgID 为单节点入参，服务层展开为 OrgIDs（nil=不过滤，非 nil 空=组织不存在）。
type AssetQuery struct {
	OrgID     string
	OrgIDs    []string
	Q         string
	Scope     string   // all|unassigned|tagged
	Online    *bool    // 仅在线/仅离线
	OnlineIDs []string // 在线身份键集合（含 DUID 别名；由联动绑定归并得出）
	Limit     int      // <=0 表示不分页（内部调用用）
	Offset    int
}

// AssetStats 当前筛选条件下的服务端统计（不受分页限制）。
type AssetStats struct {
	Total      int
	Assigned   int
	Unassigned int
	Owners     int
	Depts      int
	Tagged     int
	Online     int
	Offline    int
}

// AssetRepo 资产持久化（按身份键读写）。
type AssetRepo interface {
	List(ctx context.Context, q AssetQuery) ([]Asset, error)
	Stats(ctx context.Context, q AssetQuery) (AssetStats, error)
	Get(ctx context.Context, identity string) (Asset, bool, error)
	Upsert(ctx context.Context, a Asset) error
	Delete(ctx context.Context, identity string) error
}

// ErrAssetNotFound 资产不存在（404）。
var ErrAssetNotFound = errors.New("ASSET_NOT_FOUND")

// ErrIgnoredNotFound 忽略名单中不存在该身份键（404）。
var ErrIgnoredNotFound = errors.New("IGNORED_NOT_FOUND")

// IgnoredTerminal 忽略名单条目：无需登记资产的终端（打印机/AP/临时设备）。
type IgnoredTerminal struct {
	Identity  string
	MAC       string
	DUID      string
	Reason    string
	CreatedAt time.Time
}

// IgnoredRepo 忽略名单持久化（按身份键幂等）。
type IgnoredRepo interface {
	List(ctx context.Context) ([]IgnoredTerminal, error)
	Upsert(ctx context.Context, it IgnoredTerminal) error
	Delete(ctx context.Context, identity string) error
}

// AssetLease 资产当前租约视图（只读，由联动绑定归并得出）。
type AssetLease struct {
	IPv4     string
	IPv6     string
	State    string
	Online   bool
	LastSeen time.Time
}

// AssetBulkError 批量登记单行失败（index 0 起）。
type AssetBulkError struct {
	Index    int
	Identity string
	Code     string
	Message  string
}

// 身份键校验错误：handler 按错误值映射为 400，避免把内部错误直接回给前端。
var (
	ErrBadDUID     = errors.New("BAD_DUID")
	ErrBadIdentity = errors.New("BAD_IDENTITY")
	ErrOwnerEmpty  = errors.New("OWNER_REQUIRED")
)

// normalizeTags 规整标签数组：nil → 空数组。
// PG asset.tags 为 NOT NULL DEFAULT '{}'，但显式传 nil 会被驱动编码为 NULL 覆盖默认值而报错（M2-048）。
func normalizeTags(tags []string) []string {
	if tags == nil {
		return []string{}
	}
	return tags
}

// NormalizeDUID 归一化 DUID 文本：小写、去分隔符；只接受偶数长度十六进制（4..128）。
// 与 MAC 不同，DUID 长度不固定（LLT/LL = 14 字节，UUID = 18 字节），
// 因此不能用 coherence.NormalizeMAC（它只接受 12 位 hex，会返回空串）。
func NormalizeDUID(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
			b.WriteRune(r)
		case r == ':', r == '-', r == '.', r == ' ':
			// 分隔符忽略：方便粘贴 DUID: 前缀或分组书写的值
		default:
			return ""
		}
	}
	s := b.String()
	if len(s) < 4 || len(s) > 128 || len(s)%2 != 0 {
		return ""
	}
	return s
}

// macFromDUID 从 DUID-LLT(type 1)/DUID-LL(type 3) 推导链路层 MAC（RFC 8415 §11）。
// 布局：类型(2B) + 硬件类型(2B) + [时间(4B)] + 链路层地址；以太网硬件类型 0001 时链路层地址为 6 字节。
func macFromDUID(duid string) string {
	switch {
	case len(duid) == 28 && strings.HasPrefix(duid, "00010001"):
		// DUID-LLT：0001 | 0001 | time(8 hex) | MAC(12 hex)
		return coherence.NormalizeMAC(duid[16:])
	case len(duid) == 20 && strings.HasPrefix(duid, "00030001"):
		// DUID-LL：0003 | 0001 | MAC(12 hex)
		return coherence.NormalizeMAC(duid[8:])
	}
	return ""
}

// resolveBindingIdentity 解析联动绑定的身份：MAC 优先；否则按 DUID 查映射/推导链路层地址。
// 返回 (mac, duid)：mac 为空表示只能以 DUID 作为身份键（如 DUID-UUID）。
func resolveBindingIdentity(raw string, duidToMAC map[string]string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	if mac := coherence.NormalizeMAC(raw); mac != "" {
		return mac, ""
	}
	duid := NormalizeDUID(raw)
	if duid == "" {
		return "", ""
	}
	if m := duidToMAC[duid]; m != "" {
		if mac := coherence.NormalizeMAC(m); mac != "" {
			return mac, duid
		}
	}
	if mac := macFromDUID(duid); mac != "" {
		return mac, duid
	}
	return "", duid
}

// AssetService 校验身份键与 org 存在性，并产出未登记终端。
type AssetService struct {
	repo    AssetRepo
	orgs    OrgStore
	ignored IgnoredRepo
	mapping func(context.Context) (map[string]string, error)
}

func NewAssetService(repo AssetRepo, orgs OrgStore) *AssetService {
	return &AssetService{repo: repo, orgs: orgs}
}

// SetIdentityMapping 注入 DUID→MAC 映射源（dualstack_identity）；nil 时仅靠 DUID-LLT/LL 推导。
func (s *AssetService) SetIdentityMapping(fn func(context.Context) (map[string]string, error)) {
	s.mapping = fn
}

// SetIgnoredRepo 注入忽略名单仓储（M6-047）；nil 时 Discover 不做忽略过滤。
func (s *AssetService) SetIgnoredRepo(r IgnoredRepo) { s.ignored = r }

// subtreeOrgIDs 组织子树展开（含自身）；orgID 为空返回 nil（不过滤）。
func (s *AssetService) subtreeOrgIDs(orgID string) []string {
	if orgID == "" || s.orgs == nil {
		return nil
	}
	if _, ok := s.orgs.Get(orgID); !ok {
		return []string{}
	}
	return subtreeIDs(s.orgs.List(), orgID)
}

// List 资产列表（不分页）：组织按子树展开（与日志中心一致）。
func (s *AssetService) List(ctx context.Context, orgID, q string) ([]Asset, error) {
	return s.repo.List(ctx, AssetQuery{OrgIDs: s.subtreeOrgIDs(orgID), Q: q})
}

// ListPage 分页查询 + 服务端统计：两者使用同一筛选条件，KPI 不随翻页跳变。
func (s *AssetService) ListPage(ctx context.Context, in AssetQuery) ([]Asset, AssetStats, error) {
	in.OrgIDs = s.subtreeOrgIDs(in.OrgID)
	items, err := s.repo.List(ctx, in)
	if err != nil {
		return nil, AssetStats{}, err
	}
	stats, err := s.repo.Stats(ctx, in)
	if err != nil {
		return nil, AssetStats{}, err
	}
	return items, stats, nil
}

// AssetTerminalStats 终端登记概览（仪表盘 KPI 用，与「未登记终端」页同口径）。
type AssetTerminalStats struct {
	Unregistered int // 有有效租约但未登记、且未忽略
	Ignored      int // 忽略名单条目数
	Assets       int // 已登记资产数
}

// TerminalStats 复用 Discover 与忽略名单，保证仪表盘与发现页数字一致。
func (s *AssetService) TerminalStats(ctx context.Context, bindings []LedgerBinding) (AssetTerminalStats, error) {
	disc, err := s.Discover(ctx, bindings)
	if err != nil {
		return AssetTerminalStats{}, err
	}
	assets, err := s.repo.List(ctx, AssetQuery{})
	if err != nil {
		return AssetTerminalStats{}, err
	}
	ignored, err := s.ListIgnored(ctx)
	if err != nil {
		return AssetTerminalStats{}, err
	}
	return AssetTerminalStats{Unregistered: len(disc), Ignored: len(ignored), Assets: len(assets)}, nil
}

// BulkUpsert 批量登记：逐条校验与写入（部分成功），失败项按 index 返回。
// 不做整体回滚——批量登记时不应因一行格式错误把其余全部回退。
func (s *AssetService) BulkUpsert(ctx context.Context, items []Asset) (int, []AssetBulkError) {
	saved := 0
	failures := []AssetBulkError{}
	for i, a := range items {
		if _, err := s.Upsert(ctx, a); err != nil {
			failures = append(failures, AssetBulkError{
				Index: i, Identity: a.Identity(), Code: assetErrCode(err), Message: err.Error(),
			})
			continue
		}
		saved++
	}
	return saved, failures
}

// assetErrCode 把域内错误映射为稳定的失败码（批量结果与单条 400 共用语义）。
func assetErrCode(err error) string {
	switch {
	case errors.Is(err, ErrBadMAC):
		return "BAD_MAC"
	case errors.Is(err, ErrBadDUID):
		return "BAD_DUID"
	case errors.Is(err, ErrBadIdentity):
		return "BAD_IDENTITY"
	case errors.Is(err, ErrOwnerEmpty):
		return "OWNER_REQUIRED"
	case errors.Is(err, ErrOrgNotFound2):
		return "ORG_NOT_FOUND"
	default:
		return "DB_ERROR"
	}
}

// ListIgnored 忽略名单。
func (s *AssetService) ListIgnored(ctx context.Context) ([]IgnoredTerminal, error) {
	if s.ignored == nil {
		return []IgnoredTerminal{}, nil
	}
	return s.ignored.List(ctx)
}

// Ignore 加入忽略名单（身份键规则与资产登记一致）。
func (s *AssetService) Ignore(ctx context.Context, mac, duid, reason string) (IgnoredTerminal, error) {
	normMAC, normDUID, err := normalizeIdentity(mac, duid)
	if err != nil {
		return IgnoredTerminal{}, err
	}
	it := IgnoredTerminal{
		MAC: normMAC, DUID: normDUID,
		Reason:    strings.TrimSpace(reason),
		CreatedAt: time.Now().UTC(),
	}
	it.Identity = normMAC
	if it.Identity == "" {
		it.Identity = normDUID
	}
	if s.ignored == nil {
		return it, nil
	}
	if err := s.ignored.Upsert(ctx, it); err != nil {
		return IgnoredTerminal{}, err
	}
	return it, nil
}

// Unignore 从忽略名单移除（恢复发现）。
func (s *AssetService) Unignore(ctx context.Context, identity string) error {
	id := coherence.NormalizeMAC(identity)
	if id == "" {
		id = NormalizeDUID(identity)
	}
	if id == "" || s.ignored == nil {
		return ErrIgnoredNotFound
	}
	return s.ignored.Delete(ctx, id)
}

// LeaseIndex 用注入的 DUID→MAC 映射解析绑定身份后归并（资产页在线状态/地址列）。
func (s *AssetService) LeaseIndex(ctx context.Context, bindings []LedgerBinding) map[string]AssetLease {
	var duidToMAC map[string]string
	if s.mapping != nil {
		if m, err := s.mapping(ctx); err == nil {
			duidToMAC = m
		}
	}
	return AssetLeaseIndex(bindings, duidToMAC)
}

// AssetLeaseIndex 把联动绑定按身份键归并为资产租约视图。
//
// online 口径与仪表盘「在线租约」一致：只认 active 且 cltt+valid_lft 未过期；
// state 取该身份所有绑定中最严重者（conflict>grace>active），供资产页展示异常。
func AssetLeaseIndex(bindings []LedgerBinding, duidToMAC map[string]string) map[string]AssetLease {
	now := time.Now().Unix()
	out := make(map[string]AssetLease, len(bindings))
	for _, b := range bindings {
		state := strings.ToLower(strings.TrimSpace(b.State))
		if state != "active" && state != "grace" && state != "conflict" {
			continue
		}
		valid := b.Cltt == 0 || b.ValidLft == 0 || b.Cltt+int64(b.ValidLft) > now
		if !valid {
			continue
		}
		mac, duid := resolveBindingIdentity(b.MAC, duidToMAC)
		id := mac
		if id == "" {
			id = duid
		}
		if id == "" {
			continue
		}
		cur := out[id]
		if cur.IPv4 == "" {
			cur.IPv4 = usableAddr(b.IPv4)
		}
		if cur.IPv6 == "" {
			cur.IPv6 = usableAddr(b.IPv6)
		}
		if state == "active" {
			cur.Online = true
		}
		if stateRank(state) > stateRank(cur.State) {
			cur.State = state
		}
		if b.Cltt > 0 {
			seen := time.Unix(b.Cltt, 0).UTC()
			if seen.After(cur.LastSeen) {
				cur.LastSeen = seen
			}
		}
		// v6 绑定解析出的 MAC 身份也登记为别名，供只存 MAC 的资产行命中。
		out[id] = cur
		if duid != "" && mac != "" {
			out[duid] = cur
		}
	}
	return out
}

// normalizeIdentity 归一化入参：MAC 可解析则用 MAC 作身份键，否则回退合法 DUID。
func normalizeIdentity(mac, duid string) (string, string, error) {
	normMAC := coherence.NormalizeMAC(mac)
	if strings.TrimSpace(mac) != "" && normMAC == "" {
		return "", "", ErrBadMAC
	}
	normDUID := NormalizeDUID(duid)
	if strings.TrimSpace(duid) != "" && normDUID == "" {
		return "", "", ErrBadDUID
	}
	if normMAC == "" && normDUID == "" {
		return "", "", ErrBadIdentity
	}
	return normMAC, normDUID, nil
}

// Upsert 幂等：以身份键写入；换址不丢备注。
func (s *AssetService) Upsert(ctx context.Context, a Asset) (Asset, error) {
	normMAC, normDUID, err := normalizeIdentity(a.MAC, a.DUID)
	if err != nil {
		return Asset{}, err
	}
	if strings.TrimSpace(a.Owner) == "" {
		return Asset{}, ErrOwnerEmpty
	}
	if a.OrgID != "" {
		if _, ok := s.orgs.Get(a.OrgID); !ok {
			return Asset{}, ErrOrgNotFound2
		}
	}
	a.MAC = normMAC
	a.DUID = normDUID
	a.Owner = strings.TrimSpace(a.Owner)
	a.Tags = normalizeTags(a.Tags)
	if err := s.repo.Upsert(ctx, a); err != nil {
		return Asset{}, err
	}
	return a, nil
}

// Delete 按身份键删除（MAC 或 DUID）。
func (s *AssetService) Delete(ctx context.Context, identity string) error {
	id := coherence.NormalizeMAC(identity)
	if id == "" {
		id = NormalizeDUID(identity)
	}
	if id == "" {
		return ErrAssetNotFound
	}
	return s.repo.Delete(ctx, id)
}

// Get 按身份键读取（审计 before 快照用）。
func (s *AssetService) Get(ctx context.Context, identity string) (Asset, bool, error) {
	id := coherence.NormalizeMAC(identity)
	if id == "" {
		id = NormalizeDUID(identity)
	}
	if id == "" {
		return Asset{}, false, nil
	}
	return s.repo.Get(ctx, id)
}

// Discover 从联动绑定中筛出未登记资产（身份键 = MAC 优先 / DUID 回退），按最近租约倒序。
//
// 与旧实现的关键差异（M6-046）：
//   - DUID 先经 dualstack_identity 映射、再按 DUID-LLT/LL 推导 MAC，IPv6-only 终端不再被丢弃；
//   - 同一设备的 v4/v6 绑定按身份键合并为一条（地址/主机名/状态取并集）；
//   - 只认 active/grace，且校验 cltt+valid_lft 未过期，与仪表盘在线租约口径一致。
func (s *AssetService) Discover(ctx context.Context, bindings []LedgerBinding) ([]DiscoveredTerminal, error) {
	assets, err := s.repo.List(ctx, AssetQuery{})
	if err != nil {
		return nil, err
	}
	known := make(map[string]struct{}, len(assets)*2)
	for _, a := range assets {
		if a.MAC != "" {
			known[a.MAC] = struct{}{}
		}
		if a.DUID != "" {
			known[a.DUID] = struct{}{}
		}
	}

	// 忽略名单：Mac/DUID 两种键都记，避免解析前后身份差异导致忽略失效（M6-047）。
	if s.ignored != nil {
		if list, err := s.ignored.List(ctx); err == nil {
			for _, it := range list {
				if it.MAC != "" {
					known[it.MAC] = struct{}{}
				}
				if it.DUID != "" {
					known[it.DUID] = struct{}{}
				}
				if it.Identity != "" {
					known[it.Identity] = struct{}{}
				}
			}
		}
	}

	var duidToMAC map[string]string
	if s.mapping != nil {
		if m, err := s.mapping(ctx); err == nil {
			duidToMAC = m
		}
	}

	now := time.Now().Unix()
	byID := make(map[string]*DiscoveredTerminal, len(bindings))
	order := make([]string, 0, len(bindings))
	for _, b := range bindings {
		state := strings.ToLower(strings.TrimSpace(b.State))
		if state != "active" && state != "grace" {
			continue
		}
		// 状态未及时翻转时按租期判定，避免"假未登记"（与 M6-045 在线租约口径一致）。
		if b.Cltt > 0 && b.ValidLft > 0 && b.Cltt+int64(b.ValidLft) <= now {
			continue
		}
		mac, duid := resolveBindingIdentity(b.MAC, duidToMAC)
		if mac == "" && duid == "" {
			continue
		}
		if mac != "" {
			if _, ok := known[mac]; ok {
				continue
			}
		}
		if duid != "" {
			if _, ok := known[duid]; ok {
				continue
			}
		}
		id := mac
		if id == "" {
			id = duid
		}
		t := byID[id]
		if t == nil {
			t = &DiscoveredTerminal{Identity: id, MAC: mac, DUID: duid}
			byID[id] = t
			order = append(order, id)
		}
		mergeDiscovered(t, b)
	}

	out := make([]DiscoveredTerminal, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastSeen.Equal(out[j].LastSeen) {
			return out[i].LastSeen.After(out[j].LastSeen)
		}
		return out[i].Identity < out[j].Identity
	})
	return out, nil
}

// mergeDiscovered 合并同一身份的 v4/v6 绑定：地址/主机名/子网取首个非空，状态取最严重。
func mergeDiscovered(t *DiscoveredTerminal, b LedgerBinding) {
	if t.IPv4 == "" {
		t.IPv4 = usableAddr(b.IPv4)
	}
	if t.IPv6 == "" {
		t.IPv6 = usableAddr(b.IPv6)
	}
	if t.Hostname == "" {
		t.Hostname = strings.TrimSpace(b.Hostname)
	}
	if t.SubnetID == "" && b.SubnetID != "" {
		t.SubnetID, t.SubnetCIDR, t.OrgID = b.SubnetID, b.SubnetCIDR, b.OrgID
	}
	if stateRank(b.State) > stateRank(t.State) {
		t.State = strings.ToLower(strings.TrimSpace(b.State))
	}
	if b.Cltt > 0 {
		seen := time.Unix(b.Cltt, 0).UTC()
		if seen.After(t.LastSeen) {
			t.LastSeen = seen
			t.Cltt = b.Cltt
		}
	}
}

// stateRank 冲突 > 宽限 > 活跃，用于多绑定合并时选取代表状态。
func stateRank(state string) int {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "conflict":
		return 3
	case "grace":
		return 2
	case "active":
		return 1
	default:
		return 0
	}
}

// usableAddr 过滤未指定地址（0.0.0.0 / ::）与空值。
func usableAddr(addr string) string {
	a := strings.TrimSpace(addr)
	switch a {
	case "", "0.0.0.0", "::", "<nil>":
		return ""
	}
	return a
}
