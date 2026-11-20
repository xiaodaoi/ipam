package alerts

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	guuid "github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// Handler 实现 apigen.ServerInterface 中的报警端点（M6-040）。
type Handler struct {
	svc   *Service
	actor func(*gin.Context) string
}

// NewHandler 构造。
func NewHandler(svc *Service) *Handler {
	return &Handler{
		svc:   svc,
		actor: func(*gin.Context) string { return "" },
	}
}

// SetActorProvider 注入当前登录用户解析（main 装配 JWT 解析，避免包间依赖）。
func (h *Handler) SetActorProvider(f func(*gin.Context) string) {
	if f != nil {
		h.actor = f
	}
}

func filterOf(from, to *time.Time, q, source, kind, severity, status, intelSource *string, page, pageSize *int) Filter {
	f := Filter{}
	if from != nil {
		f.From = *from
	}
	if to != nil {
		f.To = *to
	}
	if q != nil {
		f.Q = *q
	}
	if source != nil {
		f.Source = string(*source)
	}
	if kind != nil {
		f.Kind = *kind
	}
	if severity != nil {
		f.Severity = string(*severity)
	}
	if status != nil {
		f.Status = string(*status)
	}
	if intelSource != nil {
		f.IntelSource = *intelSource
	}
	if page != nil {
		f.Page = *page
	}
	if pageSize != nil {
		f.PageSize = *pageSize
	}
	return f
}

// ListAlerts GET /logs/alerts
func (h *Handler) ListAlerts(c *gin.Context, p apigen.ListAlertsParams) {
	var src, sev, st *string
	if p.Source != nil {
		v := string(*p.Source)
		src = &v
	}
	if p.Severity != nil {
		v := string(*p.Severity)
		sev = &v
	}
	if p.Status != nil {
		v := string(*p.Status)
		st = &v
	}
	page, err := h.svc.List(c.Request.Context(), filterOf(
		p.From, p.To, p.Q, src, p.Kind, sev, st, p.IntelSource, p.Page, p.PageSize))
	if err != nil {
		writeErr(c, err)
		return
	}
	items := make([]apigen.Alert, 0, len(page.Items))
	for _, a := range page.Items {
		items = append(items, toGenAlert(a))
	}
	c.JSON(http.StatusOK, apigen.AlertList{
		Items: items, Total: page.Total, Page: page.Page, PageSize: page.PageSize,
	})
}

// GetAlertStats GET /logs/alerts/stats
func (h *Handler) GetAlertStats(c *gin.Context, p apigen.GetAlertStatsParams) {
	var src, sev, st *string
	if p.Source != nil {
		v := string(*p.Source)
		src = &v
	}
	if p.Severity != nil {
		v := string(*p.Severity)
		sev = &v
	}
	if p.Status != nil {
		v := string(*p.Status)
		st = &v
	}
	stats, err := h.svc.Stats(c.Request.Context(), filterOf(
		p.From, p.To, p.Q, src, p.Kind, sev, st, p.IntelSource, nil, nil))
	if err != nil {
		writeErr(c, err)
		return
	}
	out := apigen.AlertStats{
		Total: stats.Total, Open: stats.Open, Acked: stats.Acked, Resolved: stats.Resolved,
		BySeverity: stats.BySeverity, ByKind: stats.ByKind,
		ByIntelSource: stats.ByIntelSource,
		TopDomains:    make([]apigen.AlertAggregate, 0, len(stats.TopDomains)),
		TopClients:    make([]apigen.AlertAggregate, 0, len(stats.TopClients)),
		Trend:         make([]apigen.AlertTrendPoint, 0, len(stats.Trend)),
	}
	for _, a := range stats.TopDomains {
		out.TopDomains = append(out.TopDomains, apigen.AlertAggregate{Key: a.Key, Count: a.Count})
	}
	for _, a := range stats.TopClients {
		out.TopClients = append(out.TopClients, apigen.AlertAggregate{Key: a.Key, Count: a.Count})
	}
	for _, p := range stats.Trend {
		out.Trend = append(out.Trend, apigen.AlertTrendPoint{Ts: p.TS, Count: p.Count})
	}
	c.JSON(http.StatusOK, out)
}

// GetAlertUnread GET /logs/alerts/unread
func (h *Handler) GetAlertUnread(c *gin.Context, p apigen.GetAlertUnreadParams) {
	limit := 0
	if p.Limit != nil {
		limit = *p.Limit
	}
	unread, err := h.svc.Unread(c.Request.Context(), limit)
	if err != nil {
		writeErr(c, err)
		return
	}
	items := make([]apigen.Alert, 0, len(unread.Items))
	for _, a := range unread.Items {
		items = append(items, toGenAlert(a))
	}
	c.JSON(http.StatusOK, apigen.AlertUnread{Count: unread.Count, Items: items})
}

// AckAlerts POST /logs/alerts/ack
func (h *Handler) AckAlerts(c *gin.Context) {
	var body apigen.AlertAckRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request",
			"BAD_REQUEST", "报警 ID 列表非法")
		return
	}
	ids := make([]string, 0, len(body.AlertIds))
	for _, id := range body.AlertIds {
		ids = append(ids, id.String())
	}
	n, err := h.svc.Ack(c.Request.Context(), ids, h.actor(c))
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, apigen.AlertAckResult{Acked: n})
}

// AckAllAlerts POST /logs/alerts/ack-all
func (h *Handler) AckAllAlerts(c *gin.Context) {
	n, err := h.svc.AckAll(c.Request.Context(), h.actor(c))
	if err != nil {
		writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, apigen.AlertAckResult{Acked: n})
}

func writeErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrBadWindow), errors.Is(err, ErrBadRequest):
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request",
			"ALERT_BAD_REQUEST", err.Error())
	default:
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal",
			"ALERT_DB_ERROR", err.Error())
	}
}

func toGenAlert(a Alert) apigen.Alert {
	id, err := guuid.Parse(a.ID)
	if err != nil {
		id = guuid.Nil
	}
	out := apigen.Alert{
		Id:        openapi_types.UUID(id),
		Source:    apigen.AlertSource(a.Source),
		Kind:      a.Kind,
		Severity:  apigen.AlertSeverity(a.Severity),
		Status:    apigen.AlertStatus(a.Status),
		Title:     a.Title,
		Message:   a.Message,
		Count:     a.Count,
		FirstSeen: a.FirstSeen,
		LastSeen:  a.LastSeen,
	}
	setStr := func(dst **string, v string) {
		if v != "" {
			s := v
			*dst = &s
		}
	}
	setStr(&out.ClientIp, a.ClientIP)
	setStr(&out.ClientMac, a.ClientMAC)
	setStr(&out.Domain, a.Domain)
	setStr(&out.Component, a.Component)
	setStr(&out.IntelSource, a.IntelSource)
	setStr(&out.IntelSourceName, a.IntelSourceName)
	setStr(&out.Detail, a.Detail)
	setStr(&out.AckedBy, a.AckedBy)
	if a.AckedAt != nil {
		t := *a.AckedAt
		out.AckedAt = &t
	}
	if a.ResolvedAt != nil {
		t := *a.ResolvedAt
		out.ResolvedAt = &t
	}
	if !a.CreatedAt.IsZero() {
		t := a.CreatedAt
		out.CreatedAt = &t
	}
	return out
}
