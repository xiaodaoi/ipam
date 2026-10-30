package ipam

import (
	"context"
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
		item := apigen.DiscoveredTerminal{Mac: d.MAC}
		if d.IPv4 != "" {
			v := d.IPv4
			item.Ipv4 = &v
		}
		if d.IPv6 != "" {
			v := d.IPv6
			item.Ipv6 = &v
		}
		if d.Hostname != "" {
			v := d.Hostname
			item.Hostname = &v
		}
		if d.State != "" {
			v := d.State
			item.State = &v
		}
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
	c.JSON(http.StatusOK, apigen.AssetList{Items: items, Total: &[]int{len(items)}[0]})
}

func (h *AssetHandler) UpsertAsset(c *gin.Context) {
	var body apigen.AssetUpsert
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	in := Asset{
		MAC:   body.Mac,
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
		if strings.Contains(err.Error(), "BAD_MAC") {
			problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_MAC", "MAC 格式非法")
			return
		}
		if err == ErrOrgNotFound2 {
			problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "ORG_NOT_FOUND", "组织节点不存在")
			return
		}
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "INTERNAL", err.Error())
		return
	}
	c.JSON(http.StatusOK, toGenAsset(saved))
}

func (h *AssetHandler) DeleteAsset(c *gin.Context, mac string) {
	if err := h.svc.Delete(c.Request.Context(), mac); err != nil {
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "ASSET_NOT_FOUND", "资产不存在")
		return
	}
	c.Status(http.StatusNoContent)
}

func toGenAsset(a Asset) apigen.Asset {
	out := apigen.Asset{Mac: a.MAC, Owner: a.Owner}
	if a.OrgID != "" {
		u := uuidPtr(a.OrgID)
		out.OrgId = u
	}
	out.Dept = strPtr(a.Dept)
	out.Note = strPtr(a.Note)
	if len(a.Tags) > 0 {
		out.Tags = &a.Tags
	}
	return out
}
