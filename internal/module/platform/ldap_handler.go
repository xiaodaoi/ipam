package platform

import (
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// LDAPHandler LDAP/AD 设置与测试端点（M6-007B）。
type LDAPHandler struct {
	repo LDAPRepo
	auth *LDAPAuthenticator
}

func NewLDAPHandler(repo LDAPRepo, auth *LDAPAuthenticator) *LDAPHandler {
	return &LDAPHandler{repo: repo, auth: auth}
}

func ldapToGen(s LDAPSettings) apigen.LDAPSettings {
	bindDN, groupBase, groupFilter, groupAttr := s.BindDN, s.GroupBaseDN, s.GroupFilter, s.GroupAttr
	startTLS, skip := s.StartTLS, s.InsecureSkipVerify
	hasPw := s.BindPassword != "" || os.Getenv("IPAM_LDAP_BIND_PASSWORD") != ""
	out := apigen.LDAPSettings{
		Enabled: s.Enabled, ServerUrl: s.ServerURL, StartTls: &startTLS,
		InsecureSkipVerify: &skip, BaseDn: s.BaseDN, UserFilter: s.UserFilter,
		RoleMap: s.RoleMap, DefaultRole: s.DefaultRole, HasBindPassword: &hasPw,
	}
	if bindDN != "" {
		out.BindDn = &bindDN
	}
	if groupBase != "" {
		out.GroupBaseDn = &groupBase
	}
	if groupFilter != "" {
		out.GroupFilter = &groupFilter
	}
	if groupAttr != "" {
		out.GroupAttr = &groupAttr
	}
	if !s.UpdatedAt.IsZero() {
		t := s.UpdatedAt
		out.UpdatedAt = &t
	}
	return out
}

// GetLDAPSettings GET /system/ldap
func (h *LDAPHandler) GetLDAPSettings(c *gin.Context) {
	s, err := h.repo.Get(c.Request.Context())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.JSON(http.StatusOK, ldapToGen(s))
}

// UpdateLDAPSettings PUT /system/ldap
func (h *LDAPHandler) UpdateLDAPSettings(c *gin.Context) {
	var body apigen.LDAPSettings
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	cur, err := h.repo.Get(c.Request.Context())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	cur.Enabled = body.Enabled
	cur.ServerURL = strings.TrimSpace(body.ServerUrl)
	cur.BaseDN = strings.TrimSpace(body.BaseDn)
	cur.UserFilter = strings.TrimSpace(body.UserFilter)
	cur.DefaultRole = strings.TrimSpace(body.DefaultRole)
	if cur.UserFilter == "" {
		cur.UserFilter = "(uid={username})"
	}
	if cur.DefaultRole == "" {
		cur.DefaultRole = "user"
	}
	if body.StartTls != nil {
		cur.StartTLS = *body.StartTls
	}
	if body.InsecureSkipVerify != nil {
		cur.InsecureSkipVerify = *body.InsecureSkipVerify
	}
	if body.BindDn != nil {
		cur.BindDN = strings.TrimSpace(*body.BindDn)
	}
	if body.BindPassword != nil && *body.BindPassword != "" {
		cur.BindPassword = *body.BindPassword
	}
	if body.GroupBaseDn != nil {
		cur.GroupBaseDN = strings.TrimSpace(*body.GroupBaseDn)
	}
	if body.GroupFilter != nil {
		cur.GroupFilter = strings.TrimSpace(*body.GroupFilter)
	}
	if body.GroupAttr != nil {
		cur.GroupAttr = strings.TrimSpace(*body.GroupAttr)
	}
	if body.RoleMap != nil {
		cur.RoleMap = body.RoleMap
	}
	if cur.Enabled && (cur.ServerURL == "" || cur.BaseDN == "") {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", "启用 LDAP 时 serverUrl 与 baseDn 必填")
		return
	}
	saved, err := h.repo.Save(c.Request.Context(), cur)
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.JSON(http.StatusOK, ldapToGen(saved))
}

// TestLDAPLogin POST /system/ldap/test
func (h *LDAPHandler) TestLDAPLogin(c *gin.Context) {
	var body apigen.LDAPTestRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	u, err := h.auth.Authenticate(c.Request.Context(), body.Username, body.Password)
	if err != nil {
		switch {
		case errors.Is(err, ErrLDAPDisabled):
			problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "LDAP_DISABLED", "LDAP 未启用")
		case errors.Is(err, ErrLDAPUserNotFound), errors.Is(err, ErrLDAPBadCredentials):
			problem.Write(c, http.StatusUnauthorized, "https://ipam.local/problems/unauthorized", "LDAP_BAD_CREDENTIALS", "目录账号或口令错误")
		default:
			problem.Write(c, http.StatusServiceUnavailable, "https://ipam.local/problems/ldap-unavailable", "LDAP_UNAVAILABLE", "目录服务不可达："+err.Error())
		}
		return
	}
	out := apigen.LDAPTestResult{
		Username: u.Username, DisplayName: u.DisplayName, Groups: u.Groups, Roles: u.Roles,
	}
	if u.Email != "" {
		out.Email = &u.Email
	}
	c.JSON(http.StatusOK, out)
}
