package dns

import (
	"net/http"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// SettingsHandler 实现 apigen.ServerInterface 中设置/缓存端点。
type SettingsHandler struct {
	svc  *SettingsService
	repo SettingsRepo
}

func NewSettingsHandler(svc *SettingsService, repo SettingsRepo) *SettingsHandler {
	return &SettingsHandler{svc: svc, repo: repo}
}

func (h *SettingsHandler) GetDnsSettings(c *gin.Context) {
	s, err := h.svc.Get(c.Request.Context())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.JSON(http.StatusOK, toGenSettings(s))
}

func (h *SettingsHandler) UpdateDnsSettings(c *gin.Context) {
	var body apigen.DnsSettings
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	in := Settings{
		CacheMinTtl:    body.CacheMinTtl,
		CacheMaxTtl:    body.CacheMaxTtl,
		ServeExpired:   body.ServeExpired,
		RrlEnabled:     body.RrlEnabled,
		RrlRate:        body.RrlRate,
		DnssecValidate: body.DnssecValidate,
		TcpOnly:        deref(body.TcpOnly, false),
	}
	if err := h.svc.Update(c.Request.Context(), in); err != nil {
		if err == ErrInvalidConf {
			problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/invalid-conf", "INVALID_CONF", "配置校验失败，未生效")
			return
		}
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "INTERNAL", err.Error())
		return
	}
	c.JSON(http.StatusOK, toGenSettings(in))
}

func (h *SettingsHandler) ListTtlOverrides(c *gin.Context) {
	list, err := h.repo.ListTtlOverrides(c.Request.Context())
	if err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	items := make([]apigen.PerDomainTtl, 0, len(list))
	for _, o := range list {
		items = append(items, apigen.PerDomainTtl{Domain: o.Domain, Ttl: o.TTL})
	}
	c.JSON(http.StatusOK, struct {
		Items []apigen.PerDomainTtl `json:"items"`
	}{Items: items})
}

func (h *SettingsHandler) UpsertTtlOverride(c *gin.Context) {
	var body apigen.PerDomainTtl
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	if err := h.repo.UpsertTtlOverride(c.Request.Context(), TtlOverride{Domain: body.Domain, TTL: body.Ttl}); err != nil {
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.JSON(http.StatusOK, apigen.PerDomainTtl{Domain: body.Domain, Ttl: body.Ttl})
}

func (h *SettingsHandler) FlushCache(c *gin.Context) {
	var body struct {
		Zone *string `json:"zone"`
	}
	_ = c.ShouldBindJSON(&body)
	zone := ""
	if body.Zone != nil {
		zone = *body.Zone
	}
	flushed, cmd, err := h.svc.Flush(c.Request.Context(), zone)
	if err != nil {
		problem.Write(c, http.StatusServiceUnavailable, "https://ipam.local/problems/unbound-down", "UNBOUND_DOWN", "清空失败")
		return
	}
	c.JSON(http.StatusOK, apigen.FlushResult{Flushed: flushed, Command: cmd})
}

func toGenSettings(s Settings) apigen.DnsSettings {
	tcpOnly := s.TcpOnly
	return apigen.DnsSettings{
		CacheMinTtl:    s.CacheMinTtl,
		CacheMaxTtl:    s.CacheMaxTtl,
		ServeExpired:   s.ServeExpired,
		RrlEnabled:     s.RrlEnabled,
		RrlRate:        s.RrlRate,
		DnssecValidate: s.DnssecValidate,
		TcpOnly:        &tcpOnly,
	}
}
