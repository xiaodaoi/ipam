package ipam

import (
	"errors"
	"net/http"
	"strings"

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
		Options:     optionsFromGen(body.Options),
		Description: derefStr(body.Description),
		Gateway:       derefStr(body.Gateway),
		DNSServers:    derefStr(body.DnsServers),
		ValidLifetime: deref(body.ValidLifetime, 3600),
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
	// 以现有子网为基，body 指针字段仅在有值时覆盖（PATCH 合并语义；全量覆盖会清空未传字段并撞唯一键）
	cur, ok, err := h.svc.repo.Get(c.Request.Context(), guuid.UUID(subnetId).String())
	if err != nil || !ok {
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "SUBNET_NOT_FOUND", "子网不存在")
		return
	}
	in := Subnet{
		ID:            cur.ID,
		OrgID:         cur.OrgID,
		Family:        cur.Family,
		CIDR:          cur.CIDR,
		KeaSubnetID:   cur.KeaSubnetID,
		Name:          cur.Name,
		Description:   cur.Description,
		Gateway:       cur.Gateway,
		DNSServers:    cur.DNSServers,
		ValidLifetime: cur.ValidLifetime,
		Pools:         cur.Pools,
		Options:       cur.Options,
	}
	if body.Name != nil {
		in.Name = *body.Name
	}
	if body.Description != nil {
		in.Description = *body.Description
	}
	if body.Gateway != nil {
		in.Gateway = *body.Gateway
	}
	if body.DnsServers != nil {
		in.DNSServers = *body.DnsServers
	}
	if body.ValidLifetime != nil {
		in.ValidLifetime = *body.ValidLifetime
	}
	if body.Pools != nil {
		in.Pools = poolsFromGen(body.Pools)
	}
	if body.Options != nil {
		in.Options = optionsFromGen(body.Options)
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

func optionsFromGen(opts *[]apigen.SubnetOption) []SubnetOption {
	if opts == nil {
		return nil
	}
	out := make([]SubnetOption, 0, len(*opts))
	for _, o := range *opts {
		csv := true
		if o.CsvFormat != nil {
			csv = *o.CsvFormat
		}
		enabled := true
		if o.Enabled != nil {
			enabled = *o.Enabled
		}
		out = append(out, SubnetOption{Code: o.Code, Name: derefStr(o.Name), Data: o.Data, CSVFormat: csv, Enabled: enabled})
	}
	return out
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
	options := make([]apigen.SubnetOption, 0, len(s.Options))
	for _, o := range s.Options {
		options = append(options, apigen.SubnetOption{Code: o.Code, Name: strPtr(o.Name), Data: o.Data, CsvFormat: &o.CSVFormat, Enabled: &o.Enabled})
	}
	return apigen.Subnet{
		Id:          guuid.MustParse(s.ID),
		OrgId:       orgID,
		Name:        s.Name,
		Family:      apigen.SubnetFamily(s.Family),
		Cidr:          s.CIDR,
		Options:       &options,
		ValidLifetime: &s.ValidLifetime,
		Gateway:       &s.Gateway,
		DnsServers:    &s.DNSServers,
		Pools:       pools,
		KeaSubnetId: &[]int{s.KeaSubnetID}[0],
		Description: strPtr(s.Description),
	}
}

func deref[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

func mapSubnetErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrSubnetNotFound):
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "SUBNET_NOT_FOUND", "子网不存在")
	case errors.Is(err, ErrOrgNotFound2):
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "ORG_NOT_FOUND", "组织节点不存在")
	case errors.Is(err, ErrKeaDown):
		problem.Write(c, http.StatusServiceUnavailable, "https://ipam.local/problems/kea-down", "KEA_DOWN", "Kea 配置下发失败，已回滚至上一版本")
	case errors.Is(err, ErrSubnetDup):
		problem.Write(c, http.StatusConflict, "https://ipam.local/problems/subnet-dup", "SUBNET_DUP", "该网段已存在（CIDR 重复，Kea 要求前缀唯一）")
	case errors.Is(err, ErrBadCIDR), errors.Is(err, ErrFamilyMismatch):
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_SUBNET", err.Error())
	case strings.Contains(err.Error(), "23505"):
		problem.Write(c, http.StatusConflict, "https://ipam.local/problems/subnet-name-dup", "SUBNET_NAME_DUP", "同组织下同名子网已存在")
	default:
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "INTERNAL", err.Error())
	}
}
