package ipam

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
)

// Pool 地址池（§13.4：dynamic/pd/excluded 三类）。
type Pool struct {
	StartAddr string
	EndAddr   string
	Kind      string
}

// Subnet 子网领域对象（PG subnet 行 + 池列表）。
type Subnet struct {
	ID          string
	OrgID       string
	Name        string
	Family      int
	CIDR        string
	Pools       []Pool
	KeaSubnetID int
	Description string
}

// SubnetRepo 子网持久化抽象。（engine/kea 实现；测试用 fake）。
type KeaDeployer interface {
	// DeploySubnet 全量子网段配置下发；返回 kea 分配的 subnet-id。
	// dryRun=true 时仅生成与校验，不发网络调用。
	DeploySubnet(ctx context.Context, subnets []Subnet, dryRun bool) (int, error)
	RemoveSubnet(ctx context.Context, subnetID int) error
}

var (
	ErrSubnetNotFound = errors.New("SUBNET_NOT_FOUND")
	ErrSubnetInUse    = errors.New("SUBNET_IN_USE")
	ErrOrgNotFound2   = errors.New("ORG_NOT_FOUND")
	ErrBadCIDR        = errors.New("BAD_CIDR")
	ErrFamilyMismatch = errors.New("FAMILY_MISMATCH")
	ErrKeaDown        = errors.New("KEA_DOWN")
)

// SubnetRepo 子网持久化抽象。
type SubnetRepo interface {
	List(ctx context.Context, orgID string, family int) ([]Subnet, error)
	Get(ctx context.Context, id string) (Subnet, bool, error)
	Create(ctx context.Context, s Subnet) (Subnet, error)
	Update(ctx context.Context, s Subnet) (Subnet, error)
	Delete(ctx context.Context, id string) error
}

// SubnetService 业务规则：org 存在性、CIDR/族校验、Kea 下发与回滚。
type SubnetService struct {
	repo  SubnetRepo
	orgs  OrgStore
	kea   KeaDeployer
	apply func(ctx context.Context) error
}

func NewSubnetService(repo SubnetRepo, orgs OrgStore, kea KeaDeployer, apply func(ctx context.Context) error) *SubnetService {
	return &SubnetService{repo: repo, orgs: orgs, kea: kea, apply: apply}
}

// notifyApply 统一下发触发（M3-007 配置式：整段 config-set；apply 未注入则跳过）。
func (s *SubnetService) notifyApply(ctx context.Context) error {
	if s.apply == nil {
		return nil
	}
	return s.apply(ctx)
}

func validateSubnet(s Subnet) error {
	p, err := netip.ParsePrefix(s.CIDR)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrBadCIDR, s.CIDR)
	}
	want := 6
	if p.Addr().Is4() {
		want = 4
	}
	if s.Family != want {
		return fmt.Errorf("%w: cidr is v%d but family=%d", ErrFamilyMismatch, want, s.Family)
	}
	return nil
}

// Create 校验→Kea 下发→落库；下发失败不落库（视为回滚）。
func (s *SubnetService) Create(ctx context.Context, in Subnet, dryRun bool) (Subnet, error) {
	if in.OrgID != "" {
		if _, ok := s.orgs.Get(in.OrgID); !ok {
			return Subnet{}, ErrOrgNotFound2
		}
	}
	if err := validateSubnet(in); err != nil {
		return Subnet{}, err
	}
	// KeaSubnetID 本地持久分配（M3-007：修复原"读回 kea 第一个 id"在多子网场景的撞车）
	existing, err := s.repo.List(ctx, "", 0)
	if err != nil {
		return Subnet{}, err
	}
	maxID := 0
	for i := range existing {
		if existing[i].KeaSubnetID > maxID {
			maxID = existing[i].KeaSubnetID
		}
	}
	in.KeaSubnetID = maxID + 1
	if _, err := s.kea.DeploySubnet(ctx, []Subnet{in}, true); err != nil { // dryRun 本地校验
		return Subnet{}, ErrKeaDown
	}
	if dryRun {
		return in, nil
	}
	saved, err := s.repo.Create(ctx, in)
	if err != nil {
		return Subnet{}, err
	}
	if err := s.notifyApply(ctx); err != nil {
		_ = s.repo.Delete(ctx, saved.ID) // 下发失败回滚落库（沿严格语义）
		return Subnet{}, ErrKeaDown
	}
	return saved, nil
}

// Update 快照旧值→更新→下发；失败回滚 DB 旧值。
func (s *SubnetService) Update(ctx context.Context, id string, in Subnet) (Subnet, error) {
	cur, ok, err := s.repo.Get(ctx, id)
	if err != nil || !ok {
		return Subnet{}, ErrSubnetNotFound
	}
	in.ID = id
	in.OrgID = cur.OrgID
	in.Family = cur.Family
	in.CIDR = cur.CIDR
	in.KeaSubnetID = cur.KeaSubnetID
	if err := validateSubnet(in); err != nil {
		return Subnet{}, err
	}
	next, err := s.repo.Update(ctx, in)
	if err != nil {
		return Subnet{}, err
	}
	if _, err := s.kea.DeploySubnet(ctx, []Subnet{next}, true); err != nil { // dryRun 本地校验
		_, _ = s.repo.Update(ctx, cur) // 回滚 DB
		return Subnet{}, ErrKeaDown
	}
	if err := s.notifyApply(ctx); err != nil {
		_, _ = s.repo.Update(ctx, cur) // 回滚 DB 旧值
		return Subnet{}, ErrKeaDown
	}
	return next, nil
}

// Delete 级联删除；Kea 先摘除再删库。
func (s *SubnetService) Delete(ctx context.Context, id string) error {
	cur, ok, err := s.repo.Get(ctx, id)
	if err != nil || !ok {
		return ErrSubnetNotFound
	}
	if err := s.kea.RemoveSubnet(ctx, cur.KeaSubnetID); err != nil {
		return ErrKeaDown
	}
	return s.repo.Delete(ctx, id)
}
