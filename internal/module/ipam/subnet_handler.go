package ipam

import (
	"errors"
	"net/http"

	guuid "github.com/google/uuid"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// SubnetHandler 实现 apigen.ServerInterface 中 subnet 域端点。
type SubnetHandler struct {
	svc *SubnetService
}

func NewSubnetHandler(svc *SubnetService) *SubnetHandler { return &SubnetHandler{svc: svc} }

func (h *SubnetHandler) ListSubnets(c *gin.Context, params apigen.ListSubnetsParams) {
	orgID := ""
	if params.OrgId != nil {
		orgID = params.OrgId.String()
	}
	family := 0
	if params.Family != nil {
		family = int(*params.Family)
	}
	list, err := h.svc.repo.List(c.Request.Context(), orgID, family)
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	items := make([]apigen.Subnet, 0, len(list))
	for _, s := range list {
		items = append(items, toGenSubnet(s))
	}
	c.JSON(http.StatusOK, apigen.SubnetList{Items: items, Total: &[]int{len(items)}[0]})
}

func (h *SubnetHandler) CreateSubnet(c *gin.Context) {
	var body apigen.SubnetCreate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	dry := body.DryRun != nil && *body.DryRun
	in := Subnet{
		OrgID:       body.OrgId.String(),
		Name:        body.Name,
		Family:      int(body.Family),
		CIDR:        body.Cidr,
		Pools:       poolsFromGen(body.Pools),
		Description: derefStr(body.Description),
	}
	saved, err := h.svc.Create(c.Request.Context(), in, dry)
	if err != nil {
		mapSubnetErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, toGenSubnet(saved))
}

func (h *SubnetHandler) UpdateSubnet(c *gin.Context, subnetId apigen.SubnetIdParam) {
	var body apigen.SubnetUpdate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	in := Subnet{
		Name:        derefStr(body.Name),
		Pools:       poolsFromGen(body.Pools),
		Description: derefStr(body.Description),
	}
	next, err := h.svc.Update(c.Request.Context(), guuid.UUID(subnetId).String(), in)
	if err != nil {
		mapSubnetErr(c, err)
		return
	}
	c.JSON(http.StatusOK, toGenSubnet(next))
}

func (h *SubnetHandler) DeleteSubnet(c *gin.Context, subnetId apigen.SubnetIdParam) {
	if err := h.svc.Delete(c.Request.Context(), guuid.UUID(subnetId).String()); err != nil {
		mapSubnetErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func poolsFromGen(pools *[]apigen.AddressPool) []Pool {
	if pools == nil {
		return nil
	}
	out := make([]Pool, 0, len(*pools))
	for _, p := range *pools {
		kind := "dynamic"
		if p.Kind != nil {
			kind = string(*p.Kind)
		}
		out = append(out, Pool{StartAddr: p.StartAddr, EndAddr: p.EndAddr, Kind: kind, PrefixLen: p.PrefixLen, DelegatedLen: p.DelegatedLen})
	}
	return out
}

func toGenSubnet(s Subnet) apigen.Subnet {
	pools := []apigen.AddressPool{}
	for _, p := range s.Pools {
		k := apigen.AddressPoolKind(p.Kind)
		pools = append(pools, apigen.AddressPool{StartAddr: p.StartAddr, EndAddr: p.EndAddr, Kind: &k, PrefixLen: p.PrefixLen, DelegatedLen: p.DelegatedLen})
	}
	orgID := guuid.MustParse(s.OrgID)
	return apigen.Subnet{
		Id:          guuid.MustParse(s.ID),
		OrgId:       orgID,
		Name:        s.Name,
		Family:      apigen.SubnetFamily(s.Family),
		Cidr:        s.CIDR,
		Pools:       pools,
		KeaSubnetId: &[]int{s.KeaSubnetID}[0],
		Description: strPtr(s.Description),
	}
}

func mapSubnetErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrSubnetNotFound):
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "SUBNET_NOT_FOUND", "子网不存在")
	case errors.Is(err, ErrOrgNotFound2):
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "ORG_NOT_FOUND", "组织节点不存在")
	case errors.Is(err, ErrKeaDown):
		problem.Write(c, http.StatusServiceUnavailable, "https://ipam.local/problems/kea-down", "KEA_DOWN", "Kea 配置下发失败，已回滚至上一版本")
	case errors.Is(err, ErrBadCIDR), errors.Is(err, ErrFamilyMismatch):
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_SUBNET", err.Error())
	default:
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "INTERNAL", err.Error())
	}
}
