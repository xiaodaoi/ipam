package platform

import (
	"context"
	"errors"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
	guuid "github.com/google/uuid"
	rtypes "github.com/oapi-codegen/runtime/types"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
)

// UserHandler 实现 apigen.ServerInterface 中 platform 域用户端点（M5-004，§13.4 系统管理）。
// 读操作任何已认证用户可用；写操作由 RBAC 中间件（M5-003）限制为 admin。
type UserHandler struct{ store UserStore }

func NewUserHandler(store UserStore) *UserHandler { return &UserHandler{store: store} }

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]{2,64}$`)

// actorOf 当前操作者用户名（JWT Sub；RBAC 已保证写操作为 admin）。
func actorOf(c *gin.Context) string {
	if claims, err := ParseJWT(bearerOf(c.GetHeader("Authorization"))); err == nil {
		return claims.Sub
	}
	return ""
}

func containsRole(rs []string, role string) bool {
	for _, r := range rs {
		if r == role {
			return true
		}
	}
	return false
}

func toGenUser(u User) apigen.User {
	id := guuid.MustParse(u.ID)
	roles := make([]apigen.UserRoles, 0, len(u.Roles))
	for _, r := range u.Roles {
		roles = append(roles, apigen.UserRoles(r))
	}
	return apigen.User{
		Id: id, Username: u.Username, DisplayName: u.DisplayName,
		Roles: roles, Enabled: u.Enabled, CreatedAt: u.CreatedAt,
	}
}

func badRequest(c *gin.Context, code, detail string) {
	WriteProblem(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", code, detail)
}

// ListUsers GET /users
func (h *UserHandler) ListUsers(c *gin.Context) {
	users, err := h.store.List(c.Request.Context())
	if err != nil {
		WriteProblem(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	items := make([]apigen.User, 0, len(users))
	for _, u := range users {
		items = append(items, toGenUser(u))
	}
	c.JSON(http.StatusOK, apigen.UserList{Items: items})
}

// CreateUser POST /users
func (h *UserHandler) CreateUser(c *gin.Context) {
	var body apigen.UserCreate
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, "BAD_REQUEST", err.Error())
		return
	}
	if !usernameRe.MatchString(body.Username) {
		badRequest(c, "BAD_REQUEST", "用户名须为 2-64 位字母数字与 _.- 组合")
		return
	}
	if len(body.Password) < 8 {
		badRequest(c, "BAD_REQUEST", "口令至少 8 位")
		return
	}
	var roles []string
	if body.Roles != nil {
		for _, r := range *body.Roles {
			roles = append(roles, string(r))
		}
	}
	displayName := ""
	if body.DisplayName != nil {
		displayName = *body.DisplayName
	}
	created, err := h.store.Create(c.Request.Context(), UserCreateInput{
		Username: body.Username, DisplayName: displayName, Password: body.Password, Roles: roles,
	})
	if errors.Is(err, ErrUsernameTaken) {
		badRequest(c, "USERNAME_TAKEN", err.Error())
		return
	}
	if err != nil {
		WriteProblem(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.JSON(http.StatusCreated, toGenUser(created))
}

// lastAdminGuard 目标是启用中的 admin 且本次改动会使其失去 admin/停用/被删时，
// 须存在其他启用中的 admin（防锁死）。
func (h *UserHandler) lastAdminGuard(ctx context.Context, target UserRecord, losesAdmin bool) error {
	if !losesAdmin || !target.Enabled || !containsRole(target.Roles, "admin") {
		return nil
	}
	users, err := h.store.List(ctx)
	if err != nil {
		return err
	}
	for _, u := range users {
		if u.ID != target.ID && u.Enabled && containsRole(u.Roles, "admin") {
			return nil
		}
	}
	return errors.New("最后一名启用中的 admin 不可停用/降级/删除")
}

// UpdateUser PATCH /users/{userId}
func (h *UserHandler) UpdateUser(c *gin.Context, userId rtypes.UUID) {
	var body apigen.UserUpdate
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, "BAD_REQUEST", err.Error())
		return
	}
	target, ok, err := h.store.GetByID(c.Request.Context(), userId.String())
	if err != nil {
		WriteProblem(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	if !ok {
		WriteProblem(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "USER_NOT_FOUND", "用户不存在")
		return
	}
	if target.Username == actorOf(c) {
		if body.Roles != nil {
			badRequest(c, "USER_SELF_MANAGED", "不能变更自己的角色（需其他管理员操作）")
			return
		}
		if body.Enabled != nil && !*body.Enabled {
			badRequest(c, "USER_SELF_MANAGED", "不能停用自己的账号")
			return
		}
	}
	losesAdmin := (body.Enabled != nil && !*body.Enabled) ||
		(body.Roles != nil && !containsRole(func() []string {
			rs := make([]string, 0, len(*body.Roles))
			for _, r := range *body.Roles {
				rs = append(rs, string(r))
			}
			return rs
		}(), "admin"))
	if err := h.lastAdminGuard(c.Request.Context(), target, losesAdmin); err != nil {
		badRequest(c, "LAST_ADMIN", err.Error())
		return
	}
	if body.Password != nil && len(*body.Password) < 8 {
		badRequest(c, "BAD_REQUEST", "口令至少 8 位")
		return
	}
	in := UserUpdateInput{DisplayName: body.DisplayName, Password: body.Password, Enabled: body.Enabled}
	if body.Roles != nil {
		rs := make([]string, 0, len(*body.Roles))
		for _, r := range *body.Roles {
			rs = append(rs, string(r))
		}
		in.Roles = &rs
	}
	updated, err := h.store.Update(c.Request.Context(), userId.String(), in)
	if err != nil {
		WriteProblem(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.JSON(http.StatusOK, toGenUser(updated))
}

// DeleteUser DELETE /users/{userId}
func (h *UserHandler) DeleteUser(c *gin.Context, userId rtypes.UUID) {
	target, ok, err := h.store.GetByID(c.Request.Context(), userId.String())
	if err != nil {
		WriteProblem(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	if !ok {
		WriteProblem(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "USER_NOT_FOUND", "用户不存在")
		return
	}
	if target.Username == actorOf(c) {
		badRequest(c, "USER_SELF_MANAGED", "不能删除自己的账号")
		return
	}
	losesAdmin := target.Enabled && containsRole(target.Roles, "admin")
	if err := h.lastAdminGuard(c.Request.Context(), target, losesAdmin); err != nil {
		badRequest(c, "LAST_ADMIN", err.Error())
		return
	}
	if err := h.store.Delete(c.Request.Context(), userId.String()); err != nil {
		WriteProblem(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}
