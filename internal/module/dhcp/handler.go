package dhcp

import (
	"context"
	"errors"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
	guuid "github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	rtypes "github.com/oapi-codegen/runtime/types"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// Handler 实现 apigen.ServerInterface 中 dhcp 域端点（M2-016，C-02/C-03）。
// 写操作成功后触发 apply 下发（main 注入：BuildConfigFull + config-set）；
// 下发失败为软失败（X-Kea-Warning 头），CRUD 仍 2xx——对齐 M3-001 语义。
type Handler struct {
	store      Store
	apply      func(context.Context) error
	lease6List func(context.Context) ([]apigen.DhcpLease6, error)
}

func NewHandler(store Store, apply func(context.Context) error, lease6List func(context.Context) ([]apigen.DhcpLease6, error)) *Handler {
	return &Handler{store: store, apply: apply, lease6List: lease6List}
}

var classNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// afterApply 软失败标记：下发失败不回滚 CRUD，仅告警头。
func (h *Handler) afterApply(c *gin.Context) {
	if h.apply == nil {
		return
	}
	if err := h.apply(c.Request.Context()); err != nil {
		c.Header("X-Kea-Warning", "APPLY_FAILED")
	}
}

func badReq(c *gin.Context, code, detail string) {
	problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", code, detail)
}

func notFound(c *gin.Context, code string) {
	problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", code, "记录不存在")
}

func mustUUID(id string) rtypes.UUID { return rtypes.UUID(guuid.MustParse(id)) }

// ── 标准选项（C-02） ──────────────────────────────────

// ListDhcpOptions GET /dhcp/options
func (h *Handler) ListDhcpOptions(c *gin.Context) {
	items, err := h.store.ListOptions(c.Request.Context())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	gen := make([]apigen.DhcpOption, 0, len(items))
	for _, o := range items {
		gen = append(gen, apigen.DhcpOption{
			Id:         mustUUID(o.ID),
			OptionCode: o.OptionCode,
			Name:       o.Name,
			Data:       o.Data,
			Enabled:    o.Enabled,
		})
	}
	c.JSON(http.StatusOK, apigen.DhcpOptionList{Items: gen})
}

// CreateDhcpOption POST /dhcp/options
func (h *Handler) CreateDhcpOption(c *gin.Context) {
	var body apigen.DhcpOptionCreate
	if err := c.ShouldBindJSON(&body); err != nil {
		badReq(c, "BAD_REQUEST", err.Error())
		return
	}
	created, err := h.store.CreateOption(c.Request.Context(), DhcpOption{
		OptionCode: body.OptionCode, Name: body.Name, Data: body.Data,
		Enabled: body.Enabled == nil || *body.Enabled,
	})
	if errors.Is(err, ErrOptionTaken) {
		badReq(c, "OPTION_TAKEN", err.Error())
		return
	}
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	h.afterApply(c)
	c.JSON(http.StatusCreated, apigen.DhcpOption{
		Id: mustUUID(created.ID), OptionCode: created.OptionCode,
		Name: created.Name, Data: created.Data, Enabled: created.Enabled,
	})
}

// UpdateDhcpOption PATCH /dhcp/options/{optionId}
func (h *Handler) UpdateDhcpOption(c *gin.Context, optionId rtypes.UUID) {
	var body apigen.DhcpOptionUpdate
	if err := c.ShouldBindJSON(&body); err != nil {
		badReq(c, "BAD_REQUEST", err.Error())
		return
	}
	in := OptionUpdate{OptionCode: body.OptionCode, Name: body.Name, Data: body.Data, Enabled: body.Enabled}
	updated, err := h.store.UpdateOption(c.Request.Context(), optionId.String(), in)
	if errors.Is(err, pgx.ErrNoRows) {
		notFound(c, "DHCP_OPTION_NOT_FOUND")
		return
	}
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	h.afterApply(c)
	c.JSON(http.StatusOK, apigen.DhcpOption{
		Id: mustUUID(updated.ID), OptionCode: updated.OptionCode,
		Name: updated.Name, Data: updated.Data, Enabled: updated.Enabled,
	})
}

// DeleteDhcpOption DELETE /dhcp/options/{optionId}
func (h *Handler) DeleteDhcpOption(c *gin.Context, optionId rtypes.UUID) {
	if err := h.store.DeleteOption(c.Request.Context(), optionId.String()); err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	h.afterApply(c)
	c.Status(http.StatusNoContent)
}

// ── 类匹配规则（C-03） ──────────────────────────────

// ListDhcpClasses GET /dhcp/classes
func (h *Handler) ListDhcpClasses(c *gin.Context) {
	items, err := h.store.ListClasses(c.Request.Context())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	gen := make([]apigen.DhcpClass, 0, len(items))
	for _, cl := range items {
		gen = append(gen, toGenClass(cl))
	}
	c.JSON(http.StatusOK, apigen.DhcpClassList{Items: gen})
}

// CreateDhcpClass POST /dhcp/classes
func (h *Handler) CreateDhcpClass(c *gin.Context) {
	var body apigen.DhcpClassCreate
	if err := c.ShouldBindJSON(&body); err != nil {
		badReq(c, "BAD_REQUEST", err.Error())
		return
	}
	if !classNameRe.MatchString(body.Name) {
		badReq(c, "BAD_REQUEST", "类名须为 1-64 位字母数字与 _- 组合")
		return
	}
	created, err := h.store.CreateClass(c.Request.Context(), DhcpClass{
		Name: body.Name, Test: body.Test,
		Options: fromGenClassOptions(body.Options), Enabled: body.Enabled == nil || *body.Enabled,
	})
	if errors.Is(err, ErrClassTaken) {
		badReq(c, "CLASS_TAKEN", err.Error())
		return
	}
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	h.afterApply(c)
	c.JSON(http.StatusCreated, toGenClass(created))
}

// UpdateDhcpClass PATCH /dhcp/classes/{classId}
func (h *Handler) UpdateDhcpClass(c *gin.Context, classId rtypes.UUID) {
	var body apigen.DhcpClassUpdate
	if err := c.ShouldBindJSON(&body); err != nil {
		badReq(c, "BAD_REQUEST", err.Error())
		return
	}
	in := ClassUpdate{Test: body.Test, Enabled: body.Enabled}
	if body.Options != nil {
		opts := fromGenClassOptions(*body.Options)
		in.Options = &opts
	}
	updated, err := h.store.UpdateClass(c.Request.Context(), classId.String(), in)
	if errors.Is(err, pgx.ErrNoRows) {
		notFound(c, "DHCP_CLASS_NOT_FOUND")
		return
	}
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	h.afterApply(c)
	c.JSON(http.StatusOK, toGenClass(updated))
}

// DeleteDhcpClass DELETE /dhcp/classes/{classId}
func (h *Handler) DeleteDhcpClass(c *gin.Context, classId rtypes.UUID) {
	if err := h.store.DeleteClass(c.Request.Context(), classId.String()); err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	h.afterApply(c)
	c.Status(http.StatusNoContent)
}

// ── 转换辅助 ────────────────────────────────────────

func fromGenClassOptions(src []apigen.DhcpClassOption) []ClassOption {
	out := make([]ClassOption, 0, len(src))
	for _, o := range src {
		out = append(out, ClassOption{OptionCode: o.OptionCode, Name: o.Name, Data: o.Data})
	}
	return out
}

func toGenClassOptions(src []ClassOption) []apigen.DhcpClassOption {
	out := make([]apigen.DhcpClassOption, 0, len(src))
	for _, o := range src {
		out = append(out, apigen.DhcpClassOption{OptionCode: o.OptionCode, Name: o.Name, Data: o.Data})
	}
	return out
}

func toGenClass(c DhcpClass) apigen.DhcpClass {
	return apigen.DhcpClass{
		Id: mustUUID(c.ID), Name: c.Name, Test: c.Test,
		Options: toGenClassOptions(c.Options), Enabled: c.Enabled,
	}
}

// ListDhcpLeases6 GET /dhcp/leases6（M2-022）——Kea lease6-get-all 实时投影。
func (h *Handler) ListDhcpLeases6(c *gin.Context) {
	if h.lease6List == nil {
		c.JSON(http.StatusOK, apigen.DhcpLease6List{Items: []apigen.DhcpLease6{}})
		return
	}
	items, err := h.lease6List(c.Request.Context())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "KEA_ERROR", err.Error())
		return
	}
	if items == nil {
		items = []apigen.DhcpLease6{}
	}
	c.JSON(http.StatusOK, apigen.DhcpLease6List{Items: items})
}
