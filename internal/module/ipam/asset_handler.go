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

// SetBindingSource 注入联动绑定数据源（未登记终端发现、资产在线状态用；nil 时无租约视图）。
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

// ListAssets GET /assets：组织子树筛选 + 关键字 + 在线状态，附带当前租约地址。
func (h *AssetHandler) ListAssets(c *gin.Context, params apigen.ListAssetsParams) {
	orgID := ""
	if params.OrgId != nil {
		orgID = params.OrgId.String()
	}
	q := ""
	if params.Q != nil {
		q = *params.Q
	}
	list, err := h.svc.List(c.Request.Context(), orgID, q)
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	leases := h.leaseIndex(c.Request.Context())
	items := make([]apigen.Asset, 0, len(list))
	onlineCount := 0
	for _, a := range list {
		lease := leases[a.Identity()]
		if params.Online != nil && lease.Online != *params.Online {
			continue
		}
		if lease.Online {
			onlineCount++
		}
		items = append(items, toGenAsset(a, lease))
	}
	total := len(items)
	c.JSON(http.StatusOK, apigen.AssetList{Items: items, Total: &total, OnlineCount: &onlineCount})
}

// leaseIndex 联动绑定 → 身份键租约视图；数据源不可用时返回空 map（页面只显示离线）。
func (h *AssetHandler) leaseIndex(ctx context.Context) map[string]AssetLease {
	if h.bindings == nil {
		return map[string]AssetLease{}
	}
	bindings, err := h.bindings(ctx)
	if err != nil {
		return map[string]AssetLease{}
	}
	return h.svc.LeaseIndex(ctx, bindings)
}

// UpsertAsset PUT /assets
func (h *AssetHandler) UpsertAsset(c *gin.Context) {
	var body apigen.AssetUpsert
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	saved, err := h.svc.Upsert(c.Request.Context(), assetFromUpsert(body))
	if err != nil {
		writeAssetError(c, err)
		return
	}
	c.JSON(http.StatusOK, toGenAsset(saved, AssetLease{}))
}

// BulkUpsertAssets POST /assets/bulk：逐条写入，部分成功。
func (h *AssetHandler) BulkUpsertAssets(c *gin.Context) {
	var body apigen.AssetBulkRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	if len(body.Items) == 0 {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", "items 不能为空")
		return
	}
	if len(body.Items) > 500 {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", "单次最多 500 项")
		return
	}
	items := make([]Asset, 0, len(body.Items))
	for _, it := range body.Items {
		items = append(items, assetFromUpsert(it))
	}
	saved, failures := h.svc.BulkUpsert(c.Request.Context(), items)
	out := apigen.AssetBulkResult{
		Ok:       len(failures) == 0,
		Saved:    saved,
		Failures: make([]apigen.AssetBulkFailure, 0, len(failures)),
	}
	for _, f := range failures {
		item := apigen.AssetBulkFailure{Index: f.Index, Code: f.Code, Message: f.Message}
		item.Identity = optStr(f.Identity)
		out.Failures = append(out.Failures, item)
	}
	c.JSON(http.StatusOK, out)
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

// ListIgnoredTerminals GET /assets/ignored
func (h *AssetHandler) ListIgnoredTerminals(c *gin.Context) {
	list, err := h.svc.ListIgnored(c.Request.Context())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	items := make([]apigen.IgnoredTerminal, 0, len(list))
	for _, it := range list {
		out := apigen.IgnoredTerminal{Identity: it.Identity}
		out.Mac = optStr(it.MAC)
		out.Duid = optStr(it.DUID)
		out.Reason = optStr(it.Reason)
		if !it.CreatedAt.IsZero() {
			v := it.CreatedAt
			out.CreatedAt = &v
		}
		items = append(items, out)
	}
	c.JSON(http.StatusOK, apigen.IgnoredTerminalList{Items: items})
}

// IgnoreTerminal PUT /assets/ignored
func (h *AssetHandler) IgnoreTerminal(c *gin.Context) {
	var body apigen.IgnoredTerminalUpsert
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	it, err := h.svc.Ignore(c.Request.Context(), derefStr(body.Mac), derefStr(body.Duid), derefStr(body.Reason))
	if err != nil {
		writeAssetError(c, err)
		return
	}
	out := apigen.IgnoredTerminal{Identity: it.Identity}
	out.Mac = optStr(it.MAC)
	out.Duid = optStr(it.DUID)
	out.Reason = optStr(it.Reason)
	if !it.CreatedAt.IsZero() {
		v := it.CreatedAt
		out.CreatedAt = &v
	}
	c.JSON(http.StatusOK, out)
}

// DeleteIgnoredTerminal DELETE /assets/ignored/{identity}：恢复发现。
func (h *AssetHandler) DeleteIgnoredTerminal(c *gin.Context, identity string) {
	if err := h.svc.Unignore(c.Request.Context(), identity); err != nil {
		if errors.Is(err, ErrIgnoredNotFound) {
			problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "IGNORED_NOT_FOUND", "忽略名单中不存在该终端")
			return
		}
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "INTERNAL", err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}

// assetFromUpsert 契约 → 域模型（tags 显式传空数组，避免 PG NOT NULL 覆盖为 NULL）。
func assetFromUpsert(body apigen.AssetUpsert) Asset {
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
	return in
}

// writeAssetError 域内校验错误 → 400/404，其余 500。
func writeAssetError(c *gin.Context, err error) {
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
}

func toGenAsset(a Asset, lease AssetLease) apigen.Asset {
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
	out.Ipv4 = optStr(lease.IPv4)
	out.Ipv6 = optStr(lease.IPv6)
	out.State = optStr(lease.State)
	online := lease.Online
	out.Online = &online
	if !lease.LastSeen.IsZero() {
		v := lease.LastSeen
		out.LastSeen = &v
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
