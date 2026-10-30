package ipam

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/xiaodaoi/ipam/internal/module/coherence"
)

// Asset 资产登记（PG asset 表，MAC 身份键，§13.4）。
type Asset struct {
	MAC   string
	OrgID string
	Owner string
	Dept  string
	Note  string
	Tags  []string
}

// DiscoveredTerminal 未登记终端（有租约/绑定但 asset 表无记录）。
type DiscoveredTerminal struct {
	MAC      string
	IPv4     string
	IPv6     string
	Hostname string
	State    string
	LastSeen time.Time
}

// AssetRepo 资产持久化。
type AssetRepo interface {
	List(ctx context.Context, orgID, q string) ([]Asset, error)
	Upsert(ctx context.Context, a Asset) error
	Delete(ctx context.Context, mac string) error
}

// ErrAssetNotFound 资产不存在（404）。
var ErrAssetNotFound = errors.New("ASSET_NOT_FOUND")

// normalizeTags 规整标签数组：nil → 空数组。
// PG asset.tags 为 NOT NULL DEFAULT '{}'，但显式传 nil 会被驱动编码为 NULL 覆盖默认值而报错（M2-048）。
func normalizeTags(tags []string) []string {
	if tags == nil {
		return []string{}
	}
	return tags
}

// AssetService 校验 MAC 归一化与 org 存在性。
type AssetService struct {
	repo AssetRepo
	orgs OrgStore
}

func NewAssetService(repo AssetRepo, orgs OrgStore) *AssetService {
	return &AssetService{repo: repo, orgs: orgs}
}

// Upsert 幂等：MAC 归一化后写入；换址不丢备注（MAC 为键）。
func (s *AssetService) Upsert(ctx context.Context, a Asset) (Asset, error) {
	norm := coherence.NormalizeMAC(a.MAC)
	if norm == "" {
		return Asset{}, errors.New("BAD_MAC")
	}
	if a.OrgID != "" {
		if _, ok := s.orgs.Get(a.OrgID); !ok {
			return Asset{}, ErrOrgNotFound2
		}
	}
	a.MAC = norm
	a.Tags = normalizeTags(a.Tags)
	if err := s.repo.Upsert(ctx, a); err != nil {
		return Asset{}, err
	}
	return a, nil
}

// Delete 按 MAC 删除。
func (s *AssetService) Delete(ctx context.Context, mac string) error {
	return s.repo.Delete(ctx, coherence.NormalizeMAC(mac))
}

// Discover 从联动绑定中筛出未登记资产（MAC 为身份键），按最近活动倒序。
func (s *AssetService) Discover(ctx context.Context, bindings []LedgerBinding) ([]DiscoveredTerminal, error) {
	assets, err := s.repo.List(ctx, "", "")
	if err != nil {
		return nil, err
	}
	known := make(map[string]struct{}, len(assets))
	for _, a := range assets {
		known[a.MAC] = struct{}{}
	}
	seen := map[string]struct{}{}
	out := make([]DiscoveredTerminal, 0)
	for _, b := range bindings {
		mac := coherence.NormalizeMAC(b.MAC)
		if mac == "" || b.State == "inactive" {
			continue
		}
		if _, ok := known[mac]; ok {
			continue
		}
		if _, ok := seen[mac]; ok {
			continue
		}
		seen[mac] = struct{}{}
		ipv6 := b.IPv6
		if ipv6 == "::" {
			ipv6 = ""
		}
		var lastSeen time.Time
		if b.Cltt > 0 {
			lastSeen = time.Unix(b.Cltt, 0).UTC()
		}
		out = append(out, DiscoveredTerminal{
			MAC: mac, IPv4: b.IPv4, IPv6: ipv6, Hostname: b.Hostname,
			State: b.State, LastSeen: lastSeen,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out, nil
}
