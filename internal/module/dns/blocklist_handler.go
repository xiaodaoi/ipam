package dns

import (
	"net/http"

	guuid "github.com/google/uuid"
	rtypes "github.com/oapi-codegen/runtime/types"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// BlocklistHandler 实现 apigen.ServerInterface 中 blocklist/policy 端点。
type BlocklistHandler struct {
	svc *BlocklistService
}

func NewBlocklistHandler(svc *BlocklistService) *BlocklistHandler { return &BlocklistHandler{svc: svc} }

func (h *BlocklistHandler) ListBlocklists(c *gin.Context) {
	list, err := h.svc.repo.List(c.Request.Context())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	items := make([]apigen.Blocklist, 0, len(list))
	for _, b := range list {
		items = append(items, toGenBlocklist(b))
	}
	c.JSON(http.StatusOK, apigen.BlocklistList{Items: items, Total: &[]int{len(items)}[0]})
}

func (h *BlocklistHandler) CreateBlocklist(c *gin.Context) {
	var body apigen.BlocklistCreate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	kind := "custom"
	if body.Kind != nil {
		kind = string(*body.Kind)
	}
	syncURL := ""
	if body.SyncUrl != nil {
		syncURL = *body.SyncUrl
	}
	b, err := h.svc.repo.Create(c.Request.Context(), Blocklist{Name: body.Name, Kind: kind, SyncURL: syncURL})
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.JSON(http.StatusCreated, toGenBlocklist(b))
}

func (h *BlocklistHandler) SyncBlocklist(c *gin.Context, listId rtypes.UUID) {
	added, total, err := h.svc.SyncFeed(c.Request.Context(), listId.String())
	if err != nil && err.Error() == ErrFeedDown.Error() {
		problem.Write(c, http.StatusBadGateway, "https://ipam.local/problems/feed-down", "FEED_DOWN", "订阅源不可达或解析失败（旧版保留）")
		return
	}
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "INTERNAL", err.Error())
		return
	}
	c.JSON(http.StatusOK, apigen.BlocklistSyncResult{
		ListId: listId, Version: 0, Added: added, Total: total,
	})
}

func (h *BlocklistHandler) ListBlocklistEntries(c *gin.Context, listId rtypes.UUID, params apigen.ListBlocklistEntriesParams) {
	q := ""
	if params.Q != nil {
		q = *params.Q
	}
	list, err := h.svc.repo.ListEntries(c.Request.Context(), listId.String(), q)
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	items := make([]apigen.BlocklistEntry, 0, len(list))
	for _, e := range list {
		items = append(items, toGenEntry(e))
	}
	c.JSON(http.StatusOK, apigen.BlocklistEntryList{Items: items, Total: &[]int{len(items)}[0]})
}

func (h *BlocklistHandler) AddBlocklistEntry(c *gin.Context, listId rtypes.UUID) {
	var body apigen.BlocklistEntryCreate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	e := Entry{
		ListID:         listId.String(),
		TriggerType:    string(body.TriggerType),
		Pattern:        body.Pattern,
		Action:         string(body.Action),
		RedirectTarget: derefStr(body.RedirectTarget),
		Category:       derefStr(body.Category),
	}
	if _, err := h.svc.repo.UpsertEntries(c.Request.Context(), []Entry{e}); err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.JSON(http.StatusCreated, toGenEntry(e))
}

func (h *BlocklistHandler) ListPolicyGroups(c *gin.Context) {
	list, err := h.svc.repo.ListPolicyGroups(c.Request.Context())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	items := make([]apigen.PolicyGroup, 0, len(list))
	for _, g := range list {
		items = append(items, toGenPolicy(g))
	}
	c.JSON(http.StatusOK, struct {
		Items []apigen.PolicyGroup `json:"items"`
	}{Items: items})
}

func (h *BlocklistHandler) CreatePolicyGroup(c *gin.Context) {
	var body apigen.PolicyGroupCreate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	ids := make([]string, 0, len(body.ListIds))
	for _, id := range body.ListIds {
		ids = append(ids, id.String())
	}
	g, err := h.svc.repo.CreatePolicyGroup(c.Request.Context(), PolicyGroup{
		Name: body.Name, ViewName: body.ViewName, Cidrs: body.Cidrs, ListIDs: ids,
	})
	if err != nil {
		if err == ErrViewNameDup {
			problem.Write(c, http.StatusConflict, "https://ipam.local/problems/view-name-dup", "VIEW_NAME_DUP", "view 名称已存在")
			return
		}
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.JSON(http.StatusCreated, toGenPolicy(g))
}

func (h *BlocklistHandler) CompilePolicyGroup(c *gin.Context, groupId rtypes.UUID) {
	zone, n, path, cmd, err := h.svc.Compile(c.Request.Context(), groupId.String())
	if err == ErrPolicyNotFound {
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "POLICY_GROUP_NOT_FOUND", "策略分组不存在")
		return
	}
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.JSON(http.StatusOK, apigen.RpzCompileResult{Zone: zone, Entries: n, Path: path, ReloadCommand: cmd})
}

func toGenBlocklist(b Blocklist) apigen.Blocklist {
	out := apigen.Blocklist{
		Id:      *uuidPtr(b.ID),
		Name:    b.Name,
		Kind:    apigen.BlocklistKind(b.Kind),
		Version: b.Version,
	}
	if b.SyncURL != "" {
		out.SyncUrl = &b.SyncURL
	}
	if !b.LastSync.IsZero() {
		out.LastSync = &b.LastSync
	}
	return out
}

func toGenEntry(e Entry) apigen.BlocklistEntry {
	out := apigen.BlocklistEntry{
		ListId:      *uuidPtr(e.ListID),
		TriggerType: apigen.BlocklistEntryTriggerType(e.TriggerType),
		Pattern:     e.Pattern,
		Action:      apigen.BlocklistEntryAction(e.Action),
	}
	if e.RedirectTarget != "" {
		out.RedirectTarget = &e.RedirectTarget
	}
	if e.Category != "" {
		out.Category = &e.Category
	}
	return out
}

func toGenPolicy(g PolicyGroup) apigen.PolicyGroup {
	ids := make([]rtypes.UUID, 0, len(g.ListIDs))
	for _, id := range g.ListIDs {
		u := guuid.MustParse(id)
		ids = append(ids, rtypes.UUID(u))
	}
	return apigen.PolicyGroup{
		Id:       *uuidPtr(g.ID),
		Name:     g.Name,
		ViewName: g.ViewName,
		Cidrs:    g.Cidrs,
		ListIds:  ids,
	}
}
