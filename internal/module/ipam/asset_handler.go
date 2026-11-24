package ipam

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// AssetHandler 实现 apigen.ServerInterface 中 asset 域端点。
type AssetHandler struct {
	svc      *AssetService
	bindings func(context.Context) ([]LedgerBinding, error)
}

func NewAssetHandler(svc *AssetService) *AssetHandler { return &AssetHandler{svc: svc} }

// SetBindingSource 注入联动绑定数据源（未登记终端发现用；nil 时返回空列表）。
func (h *AssetHandler) SetBindingSource(fn func(context.Context) ([]LedgerBinding, error)) {
	h.bindings = fn
}

// ListDiscoveredTerminals GET /assets/discovered
func (h *AssetHandler) ListDiscoveredTerminals(c *gin.Context) {
	if h.bindings == nil {
		c.JSON(http.StatusOK, apigen.DiscoveredTerminalList{Items: []apigen.DiscoveredTerminal{}})
		return
	}
	bindings, err := h.bindings(c.Request.Context())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	list, err := h.svc.Discover(c.Request.Context(), bindings)
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	items := make([]apigen.DiscoveredTerminal, 0, len(list))
	for _, d := range list {
		item := apigen.DiscoveredTerminal{Identity: d.Identity}
		item.Mac = optStr(d.MAC)
		item.Duid = optStr(d.DUID)
		item.Ipv4 = optStr(d.IPv4)
		item.Ipv6 = optStr(d.IPv6)
		item.Hostname = optStr(d.Hostname)
		item.State = optStr(d.State)
		item.SubnetId = optStr(d.SubnetID)
		item.SubnetCidr = optStr(d.SubnetCIDR)
		item.OrgId = optStr(d.OrgID)
		if !d.LastSeen.IsZero() {
			v := d.LastSeen
			item.LastSeen = &v
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, apigen.DiscoveredTerminalList{Items: items})
}

func (h *AssetHandler) ListAssets(c *gin.Context, params apigen.ListAssetsParams) {
	orgID := ""
	if params.OrgId != nil {
		orgID = params.OrgId.String()
	}
	q := ""
	if params.Q != nil {
		q = *params.Q
	}
	list, err := h.svc.repo.List(c.Request.Context(), orgID, q)
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	items := make([]apigen.Asset, 0, len(list))
	for _, a := range list {
		items = append(items, toGenAsset(a))
	}
	total := len(items)
	c.JSON(http.StatusOK, apigen.AssetList{Items: items, Total: &total})
}

func (h *AssetHandler) UpsertAsset(c *gin.Context) {
	var body apigen.AssetUpsert
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	in := Asset{
		MAC:   derefStr(body.Mac),
		DUID:  derefStr(body.Duid),
		Owner: body.Owner,
		Dept:  derefStr(body.Dept),
		Note:  derefStr(body.Note),
	}
	if body.OrgId != nil {
		in.OrgID = body.OrgId.String()
	}
	if body.Tags != nil {
		in.Tags = *body.Tags
	}
	saved, err := h.svc.Upsert(c.Request.Context(), in)
	if err != nil {
		switch {
		case errors.Is(err, ErrBadMAC):
			problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_MAC", "MAC 格式非法（需 12 位十六进制）")
		case errors.Is(err, ErrBadDUID):
			problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_DUID", "DUID 格式非法（需偶数长度十六进制）")
		case errors.Is(err, ErrBadIdentity):
			problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_IDENTITY", "MAC 与 DUID 至少填写一个")
		case errors.Is(err, ErrOwnerEmpty):
			problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "OWNER_REQUIRED", "使用人不能为空")
		case errors.Is(err, ErrOrgNotFound2):
			problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "ORG_NOT_FOUND", "组织节点不存在")
		default:
			problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "INTERNAL", err.Error())
		}
		return
	}
	c.JSON(http.StatusOK, toGenAsset(saved))
}

// DeleteAsset DELETE /assets/{mac}：路径参数为身份键（MAC 或 DUID）。
func (h *AssetHandler) DeleteAsset(c *gin.Context, mac string) {
	if err := h.svc.Delete(c.Request.Context(), mac); err != nil {
		if errors.Is(err, ErrAssetNotFound) {
			problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "ASSET_NOT_FOUND", "资产不存在")
			return
		}
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "INTERNAL", err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}

func toGenAsset(a Asset) apigen.Asset {
	out := apigen.Asset{Identity: a.Identity(), Owner: a.Owner}
	if a.OrgID != "" {
		out.OrgId = uuidPtr(a.OrgID)
	}
	out.Mac = optStr(a.MAC)
	out.Duid = optStr(a.DUID)
	out.Dept = optStr(a.Dept)
	out.Note = optStr(a.Note)
	if len(a.Tags) > 0 {
		out.Tags = &a.Tags
	}
	return out
}

// optStr 空串转 nil：契约里这些字段是可选的，回空串会让前端显示空白列。
func optStr(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	v := s
	return &v
}
