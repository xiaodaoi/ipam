package platform

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	rtypes "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// 角色管理错误（M2-035）。
var (
	ErrRoleTaken    = errors.New("ROLE_TAKEN")
	ErrRoleNotFound = errors.New("ROLE_NOT_FOUND")
	ErrRoleBuiltin  = errors.New("BUILTIN_ROLE")
)

// validPermSet 合法权限点（六域 × 读写）。
var validPermSet = func() map[string]bool {
	m := map[string]bool{}
	for _, p := range allPermList {
		m[p] = true
	}
	return m
}()

func validatePerms(ps []string) bool {
	if len(ps) == 0 {
		return false
	}
	for _, p := range ps {
		if !validPermSet[p] {
			return false
		}
	}
	return true
}

// RolesHandler 角色管理（M2-035，system 域——中间件已拦 system:write）。
type RolesHandler struct {
	store RoleStore
}

func NewRolesHandler(store RoleStore) *RolesHandler { return &RolesHandler{store: store} }

// ListRoles GET /roles
func (h *RolesHandler) ListRoles(c *gin.Context) {
	items, err := h.store.List(c.Request.Context())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	out := make([]rtypes.Role, 0, len(items))
	for _, r := range items {
		out = append(out, toGenRole(r))
	}
	c.JSON(http.StatusOK, rtypes.RoleList{Items: out})
}

// CreateRole POST /roles——内置名保护 + 权限点校验 + RegisterRole（normalizeRoles 白名单扩展）。
func (h *RolesHandler) CreateRole(c *gin.Context) {
	var body rtypes.RoleCreate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	if builtinRolePerms[body.Name] != nil {
		problem.Write(c, http.StatusConflict, "https://ipam.local/problems/conflict", "ROLE_TAKEN", "角色名与内置角色冲突")
		return
	}
	perms := make([]string, 0, len(body.Permissions))
	for _, p := range body.Permissions {
		perms = append(perms, string(p))
	}
	if !validatePerms(perms) {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "INVALID_PERMS", "权限点非法或为空（域:read|write）")
		return
	}
	r := Role{Name: body.Name, Permissions: perms, Builtin: false}
	if err := h.store.Create(c.Request.Context(), r); err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.JSON(http.StatusCreated, toGenRole(r))
}

// UpdateRole PATCH /roles/{roleId}——builtin 409。
func (h *RolesHandler) UpdateRole(c *gin.Context, roleId string) {
	var body rtypes.RoleUpdate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	if builtinRolePerms[roleId] != nil {
		problem.Write(c, http.StatusConflict, "https://ipam.local/problems/conflict", "BUILTIN_ROLE", "内置角色不可修改")
		return
	}
	perms := make([]string, 0, len(body.Permissions))
	for _, p := range body.Permissions {
		perms = append(perms, string(p))
	}
	if !validatePerms(perms) {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "INVALID_PERMS", "权限点非法或为空（域:read|write）")
		return
	}
	if err := h.store.Update(c.Request.Context(), roleId, perms); err != nil {
		if errors.Is(err, ErrRoleNotFound) {
			problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "ROLE_NOT_FOUND", "角色不存在")
			return
		}
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	r, _, _ := h.store.Get(c.Request.Context(), roleId)
	c.JSON(http.StatusOK, toGenRole(r))
}

// DeleteRole DELETE /roles/{roleId}——builtin 409。
func (h *RolesHandler) DeleteRole(c *gin.Context, roleId string) {
	if builtinRolePerms[roleId] != nil {
		problem.Write(c, http.StatusConflict, "https://ipam.local/problems/conflict", "BUILTIN_ROLE", "内置角色不可删除")
		return
	}
	if err := h.store.Delete(c.Request.Context(), roleId); err != nil {
		if errors.Is(err, ErrRoleNotFound) {
			problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "ROLE_NOT_FOUND", "角色不存在")
			return
		}
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}

func toGenRole(r Role) rtypes.Role {
	return rtypes.Role{Name: r.Name, Permissions: r.Permissions, Builtin: r.Builtin}
}
