package ipam

import (
	"context"
	"errors"

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

// AssetRepo 资产持久化。
type AssetRepo interface {
	List(ctx context.Context, orgID, q string) ([]Asset, error)
	Upsert(ctx context.Context, a Asset) error
	Delete(ctx context.Context, mac string) error
}

// ErrAssetNotFound 资产不存在（404）。
var ErrAssetNotFound = errors.New("ASSET_NOT_FOUND")

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
	if err := s.repo.Upsert(ctx, a); err != nil {
		return Asset{}, err
	}
	return a, nil
}

// Delete 按 MAC 删除。
func (s *AssetService) Delete(ctx context.Context, mac string) error {
	return s.repo.Delete(ctx, coherence.NormalizeMAC(mac))
}
