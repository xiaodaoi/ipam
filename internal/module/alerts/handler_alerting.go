package alerts

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	guuid "github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

func uuidOf(s string) openapi_types.UUID {
	if s == "" {
		return openapi_types.UUID(guuid.Nil)
	}
	id, err := guuid.Parse(s)
	if err != nil {
		return openapi_types.UUID(guuid.Nil)
	}
	return openapi_types.UUID(id)
}

func uuidStr(id openapi_types.UUID) string { return guuid.UUID(id).String() }

func toGenDecision(raw string) *apigen.AlertDecision {
	if raw == "" {
		return nil
	}
	var d Decision
	if json.Unmarshal([]byte(raw), &d) != nil {
		return nil
	}
	out := &apigen.AlertDecision{}
	if d.PolicyID != "" {
		out.PolicyId = &d.PolicyID
	}
	if d.PolicyName != "" {
		out.PolicyName = &d.PolicyName
	}
	if d.MatchedBy != "" {
		out.MatchedBy = &d.MatchedBy
	}
	if d.Severity != "" {
		v := apigen.AlertDecisionSeverity(d.Severity)
		out.Severity = &v
	}
	if d.NotifyMode != "" {
		v := apigen.AlertNotifyMode(d.NotifyMode)
		out.NotifyMode = &v
	}
	if d.GroupBy != "" {
		v := apigen.AlertGroupBy(d.GroupBy)
		out.GroupBy = &v
	}
	notify, capped := d.Notify, d.Capped
	out.Notify, out.Capped = &notify, &capped
	if d.SuppressedBy != "" {
		out.SuppressedBy = &d.SuppressedBy
	}
	if d.Final != "" {
		out.Final = &d.Final
	}
	return out
}

func toGenPolicy(p Policy) apigen.AlertPolicy {
	out := apigen.AlertPolicy{
		Id: uuidOf(p.ID), Name: p.Name,
		ScopeType: apigen.AlertPolicyScope(p.ScopeType), ScopeKey: p.ScopeKey,
		Priority: p.Priority, Severity: apigen.AlertPolicySeverity(p.Severity),
		NotifyMode: apigen.AlertNotifyMode(p.NotifyMode),
		AlertClass: apigen.AlertClass(p.AlertClass), GroupBy: apigen.AlertGroupBy(p.GroupBy),
		MaxPerHour: p.MaxPerHour, RepeatAfterSec: p.RepeatAfterSec, ResolveAfterSec: p.ResolveAfterSec,
		Enabled: p.Enabled,
	}
	if p.CreatedBy != "" {
		out.CreatedBy = &p.CreatedBy
	}
	if p.UpdatedBy != "" {
		out.UpdatedBy = &p.UpdatedBy
	}
	if !p.CreatedAt.IsZero() {
		t := p.CreatedAt
		out.CreatedAt = &t
	}
	if !p.UpdatedAt.IsZero() {
		t := p.UpdatedAt
		out.UpdatedAt = &t
	}
	return out
}

func policyFromCreate(b apigen.AlertPolicyCreate) Policy {
	p := Policy{
		Name: b.Name, ScopeType: string(b.ScopeType), Priority: 100,
		Severity: SeverityWarning, NotifyMode: NotifyModeNotify, AlertClass: ClassPolicyBlock,
		GroupBy: GroupByDomain, RepeatAfterSec: 86400, ResolveAfterSec: 86400, Enabled: true,
	}
	if b.ScopeKey != nil {
		p.ScopeKey = *b.ScopeKey
	}
	if b.Priority != nil {
		p.Priority = *b.Priority
	}
	if b.Severity != nil {
		p.Severity = string(*b.Severity)
	}
	if b.NotifyMode != nil {
		p.NotifyMode = string(*b.NotifyMode)
	}
	if b.AlertClass != nil {
		p.AlertClass = string(*b.AlertClass)
	} else if b.ScopeType == apigen.AlertPolicyScopeIntelSource || b.ScopeType == apigen.AlertPolicyScopeBlocklist {
		// 未显式指定分类时，情报源/名单类默认为安全威胁，避免被归到「策略封禁」。
		p.AlertClass = ClassSecurityThreat
	}
	if b.GroupBy != nil {
		p.GroupBy = string(*b.GroupBy)
	}
	if b.MaxPerHour != nil {
		p.MaxPerHour = *b.MaxPerHour
	}
	if b.RepeatAfterSec != nil {
		p.RepeatAfterSec = *b.RepeatAfterSec
	}
	if b.ResolveAfterSec != nil {
		p.ResolveAfterSec = *b.ResolveAfterSec
	}
	if b.Enabled != nil {
		p.Enabled = *b.Enabled
	}
	return p
}

func patchFromUpdate(b apigen.AlertPolicyUpdate) PolicyPatch {
	p := PolicyPatch{}
	if b.Name != nil {
		p.Name = b.Name
	}
	if b.Priority != nil {
		p.Priority = b.Priority
	}
	if b.Severity != nil {
		v := string(*b.Severity)
		p.Severity = &v
	}
	if b.NotifyMode != nil {
		v := string(*b.NotifyMode)
		p.NotifyMode = &v
	}
	if b.AlertClass != nil {
		v := string(*b.AlertClass)
		p.AlertClass = &v
	}
	if b.GroupBy != nil {
		v := string(*b.GroupBy)
		p.GroupBy = &v
	}
	p.MaxPerHour, p.RepeatAfterSec, p.ResolveAfterSec = b.MaxPerHour, b.RepeatAfterSec, b.ResolveAfterSec
	p.Enabled = b.Enabled
	return p
}

func matchersFromGen(m apigen.SilenceMatchers) SilenceMatchers {
	out := SilenceMatchers{}
	if m.Source != nil {
		out.Source = *m.Source
	}
	if m.Kind != nil {
		out.Kind = *m.Kind
	}
	if m.Component != nil {
		out.Component = *m.Component
	}
	if m.IntelSource != nil {
		out.IntelSource = *m.IntelSource
	}
	if m.Severity != nil {
		out.Severity = *m.Severity
	}
	if m.ClientIp != nil {
		out.ClientIP = *m.ClientIp
	}
	if m.Domain != nil {
		out.Domain = *m.Domain
	}
	return out
}

func matchersToGen(m SilenceMatchers) apigen.SilenceMatchers {
	out := apigen.SilenceMatchers{}
	set := func(dst **string, v string) {
		if v != "" {
			x := v
			*dst = &x
		}
	}
	set(&out.Source, m.Source)
	set(&out.Kind, m.Kind)
	set(&out.Component, m.Component)
	set(&out.IntelSource, m.IntelSource)
	set(&out.Severity, m.Severity)
	set(&out.ClientIp, m.ClientIP)
	set(&out.Domain, m.Domain)
	return out
}

func toGenSilence(s Silence, now time.Time) apigen.AlertSilence {
	active := s.ActiveAt(now)
	out := apigen.AlertSilence{
		Id: uuidOf(s.ID), Matchers: matchersToGen(s.Matchers),
		StartsAt: s.StartsAt, EndsAt: s.EndsAt, Reason: s.Reason,
		CreatedBy: s.CreatedBy, Active: &active,
	}
	if !s.CreatedAt.IsZero() {
		t := s.CreatedAt
		out.CreatedAt = &t
	}
	return out
}

func toGenChannel(ch Channel) apigen.NotificationChannel {
	out := apigen.NotificationChannel{
		Id: uuidOf(ch.ID), Name: ch.Name,
		Type:        apigen.NotificationChannelType(ch.Type),
		MinSeverity: apigen.NotificationChannelMinSeverity(ch.MinSeverity),
		Enabled:     ch.Enabled, HasSecret: ch.SecretFile != "",
	}
	if ch.Config.URL != "" {
		out.Url = &ch.Config.URL
	}
	if ch.Config.SMTPHost != "" {
		out.SmtpHost = &ch.Config.SMTPHost
	}
	if ch.Config.SMTPPort > 0 {
		out.SmtpPort = &ch.Config.SMTPPort
	}
	if ch.Config.SMTPUser != "" {
		out.SmtpUser = &ch.Config.SMTPUser
	}
	if ch.Config.SMTPFrom != "" {
		out.SmtpFrom = &ch.Config.SMTPFrom
	}
	if len(ch.Config.Recipients) > 0 {
		out.Recipients = &ch.Config.Recipients
	}
	if ch.QuietHours != "" {
		out.QuietHours = &ch.QuietHours
	}
	if ch.LastStatus != "" {
		out.LastStatus = &ch.LastStatus
	}
	if ch.LastTestAt != nil {
		t := *ch.LastTestAt
		out.LastTestAt = &t
	}
	if !ch.CreatedAt.IsZero() {
		t := ch.CreatedAt
		out.CreatedAt = &t
	}
	if !ch.UpdatedAt.IsZero() {
		t := ch.UpdatedAt
		out.UpdatedAt = &t
	}
	return out
}

func toGenDelivery(d Delivery) apigen.NotificationDelivery {
	out := apigen.NotificationDelivery{
		Id: uuidOf(d.ID), AlertId: uuidOf(d.AlertID), ChannelId: uuidOf(d.ChannelID),
		ChannelName: d.ChannelName,
		Status:      apigen.NotificationDeliveryStatus(d.Status), Attempt: d.Attempt,
	}
	if d.Error != "" {
		out.Error = &d.Error
	}
	if !d.SentAt.IsZero() {
		t := d.SentAt
		out.SentAt = &t
	}
	return out
}

func writeAdminErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrBadRequest):
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "ALERT_BAD_REQUEST", err.Error())
	case errors.Is(err, ErrNotFound):
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "ALERT_NOT_FOUND", err.Error())
	case errors.Is(err, ErrConflict):
		problem.Write(c, http.StatusConflict, "https://ipam.local/problems/conflict", "ALERT_CONFLICT", err.Error())
	default:
		problem.Write(c, http.StatusInternalServerError, "https://ipam.local/problems/internal", "ALERT_DB_ERROR", err.Error())
	}
}

// GetAlertingOverview GET /alerting/overview
func (h *Handler) GetAlertingOverview(c *gin.Context, p apigen.GetAlertingOverviewParams) {
	now := time.Now().UTC()
	from, to := now.Add(-24*time.Hour), now
	if p.From != nil {
		from = *p.From
	}
	if p.To != nil {
		to = *p.To
	}
	st, err := h.svc.Stats(c.Request.Context(), Filter{From: from, To: to})
	if err != nil {
		writeAdminErr(c, err)
		return
	}
	policies, _ := h.svc.Policies(c.Request.Context())
	silences, _ := h.svc.Silences(c.Request.Context())
	channels, _ := h.svc.Channels(c.Request.Context())
	activeSilences, enabledChannels := 0, 0
	for _, s := range silences {
		if s.ActiveAt(now) {
			activeSilences++
		}
	}
	for _, ch := range channels {
		if ch.Enabled {
			enabledChannels++
		}
	}
	top := make([]apigen.AlertingSourceStat, 0, len(st.BySource))
	for _, s := range st.BySource {
		mode := apigen.AlertNotifyMode(NotifyModeDigest)
		if s.Notified > 0 {
			mode = apigen.AlertNotifyMode(NotifyModeNotify)
		}
		top = append(top, apigen.AlertingSourceStat{
			Key: s.Key, Recorded: s.Recorded, Notified: s.Notified, Mode: mode,
		})
	}
	notifiedByTS := map[time.Time]int{}
	for _, p := range st.NotifiedTrend {
		notifiedByTS[p.TS] = p.Count
	}
	trend := make([]apigen.AlertingTrendPoint, 0, len(st.Trend))
	for _, p := range st.Trend {
		trend = append(trend, apigen.AlertingTrendPoint{Ts: p.TS, Recorded: p.Count, Notified: notifiedByTS[p.TS]})
	}
	ratio := float32(0)
	if st.Total > 0 {
		ratio = 1 - float32(st.Notified)/float32(st.Total)
	}
	polN, silN, chN := len(policies), activeSilences, enabledChannels
	c.JSON(http.StatusOK, apigen.AlertingOverview{
		Recorded: st.Total, Alerts: st.Total, Notified: st.Notified, Archived: st.Archived,
		Suppressed: st.Suppressed, Open: st.Open, Acked: st.Acked, NoiseRatio: ratio,
		CriticalSuppressed: st.CriticalSuppressed, BySeverity: st.BySeverity,
		TopSources: top, Trend: trend, Policies: &polN, Silences: &silN, Channels: &chN,
	})
}

// ListAlertPolicies GET /alerting/policies
func (h *Handler) ListAlertPolicies(c *gin.Context) {
	policies, err := h.svc.Policies(c.Request.Context())
	if err != nil {
		writeAdminErr(c, err)
		return
	}
	items := make([]apigen.AlertPolicy, 0, len(policies))
	for _, p := range policies {
		items = append(items, toGenPolicy(p))
	}
	total := len(items)
	c.JSON(http.StatusOK, apigen.AlertPolicyList{Items: items, Total: &total})
}

// CreateAlertPolicy POST /alerting/policies
func (h *Handler) CreateAlertPolicy(c *gin.Context) {
	var body apigen.AlertPolicyCreate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", "策略参数非法")
		return
	}
	out, err := h.svc.CreatePolicy(c.Request.Context(), policyFromCreate(body), h.actor(c))
	if err != nil {
		writeAdminErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, toGenPolicy(out))
}

// UpdateAlertPolicy PATCH /alerting/policies/{policyId}
func (h *Handler) UpdateAlertPolicy(c *gin.Context, policyId openapi_types.UUID) {
	var body apigen.AlertPolicyUpdate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", "策略参数非法")
		return
	}
	out, err := h.svc.UpdatePolicy(c.Request.Context(), uuidStr(policyId), patchFromUpdate(body), h.actor(c))
	if err != nil {
		writeAdminErr(c, err)
		return
	}
	c.JSON(http.StatusOK, toGenPolicy(out))
}

// DeleteAlertPolicy DELETE /alerting/policies/{policyId}
func (h *Handler) DeleteAlertPolicy(c *gin.Context, policyId openapi_types.UUID) {
	if err := h.svc.DeletePolicy(c.Request.Context(), uuidStr(policyId)); err != nil {
		writeAdminErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// SimulateAlertPolicy POST /alerting/policies/simulate
func (h *Handler) SimulateAlertPolicy(c *gin.Context) {
	var body apigen.PolicySimulationRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", "模拟参数非法")
		return
	}
	var from, to time.Time
	if body.From != nil {
		from = *body.From
	}
	if body.To != nil {
		to = *body.To
	}
	res, err := h.svc.SimulatePolicy(c.Request.Context(), policyFromCreate(body.Policy), from, to)
	if err != nil {
		writeAdminErr(c, err)
		return
	}
	c.JSON(http.StatusOK, apigen.PolicySimulationResult{
		Recorded: res.Recorded, Notified: res.Notified, Archived: res.Archived,
		Suppressed: res.Suppressed, CriticalNotified: res.CriticalNotified,
		CriticalSuppressed: res.CriticalSuppressed, BySeverity: res.BySeverity,
	})
}

// ListAlertSilences GET /alerting/silences
func (h *Handler) ListAlertSilences(c *gin.Context) {
	list, err := h.svc.Silences(c.Request.Context())
	if err != nil {
		writeAdminErr(c, err)
		return
	}
	now := time.Now().UTC()
	items := make([]apigen.AlertSilence, 0, len(list))
	for _, s := range list {
		items = append(items, toGenSilence(s, now))
	}
	total := len(items)
	c.JSON(http.StatusOK, apigen.AlertSilenceList{Items: items, Total: &total})
}

// CreateAlertSilence POST /alerting/silences
func (h *Handler) CreateAlertSilence(c *gin.Context) {
	var body apigen.AlertSilenceCreate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", "静音参数非法")
		return
	}
	sil := Silence{Matchers: matchersFromGen(body.Matchers), EndsAt: body.EndsAt, Reason: body.Reason}
	if body.StartsAt != nil {
		sil.StartsAt = *body.StartsAt
	}
	out, err := h.svc.CreateSilence(c.Request.Context(), sil, h.actor(c))
	if err != nil {
		writeAdminErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, toGenSilence(out, time.Now().UTC()))
}

// DeleteAlertSilence DELETE /alerting/silences/{silenceId}
func (h *Handler) DeleteAlertSilence(c *gin.Context, silenceId openapi_types.UUID) {
	if err := h.svc.DeleteSilence(c.Request.Context(), uuidStr(silenceId)); err != nil {
		writeAdminErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ListNotificationChannels GET /alerting/channels
func (h *Handler) ListNotificationChannels(c *gin.Context) {
	list, err := h.svc.Channels(c.Request.Context())
	if err != nil {
		writeAdminErr(c, err)
		return
	}
	items := make([]apigen.NotificationChannel, 0, len(list))
	for _, ch := range list {
		items = append(items, toGenChannel(ch))
	}
	total := len(items)
	c.JSON(http.StatusOK, apigen.NotificationChannelList{Items: items, Total: &total})
}

func channelFromCreate(b apigen.NotificationChannelCreate) Channel {
	ch := Channel{
		Name: b.Name, Type: string(b.Type), MinSeverity: SeverityWarning, Enabled: true,
	}
	if b.Url != nil {
		ch.Config.URL = *b.Url
	}
	if b.SmtpHost != nil {
		ch.Config.SMTPHost = *b.SmtpHost
	}
	if b.SmtpPort != nil {
		ch.Config.SMTPPort = *b.SmtpPort
	}
	if b.SmtpUser != nil {
		ch.Config.SMTPUser = *b.SmtpUser
	}
	if b.SmtpFrom != nil {
		ch.Config.SMTPFrom = *b.SmtpFrom
	}
	if b.Recipients != nil {
		ch.Config.Recipients = *b.Recipients
	}
	if b.MinSeverity != nil {
		ch.MinSeverity = string(*b.MinSeverity)
	}
	if b.QuietHours != nil {
		ch.QuietHours = *b.QuietHours
	}
	if b.Enabled != nil {
		ch.Enabled = *b.Enabled
	}
	return ch
}

// CreateNotificationChannel POST /alerting/channels
func (h *Handler) CreateNotificationChannel(c *gin.Context) {
	var body apigen.NotificationChannelCreate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", "通道参数非法")
		return
	}
	secret := ""
	if body.Secret != nil {
		secret = *body.Secret
	}
	out, err := h.svc.CreateChannel(c.Request.Context(), channelFromCreate(body), secret, h.actor(c))
	if err != nil {
		writeAdminErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, toGenChannel(out))
}

// UpdateNotificationChannel PATCH /alerting/channels/{channelId}
func (h *Handler) UpdateNotificationChannel(c *gin.Context, channelId openapi_types.UUID) {
	var body apigen.NotificationChannelUpdate
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "BAD_REQUEST", "通道参数非法")
		return
	}
	patch := ChannelPatch{
		Name: body.Name, SMTPHost: body.SmtpHost, SMTPPort: body.SmtpPort,
		SMTPUser: body.SmtpUser, SMTPFrom: body.SmtpFrom, Recipients: body.Recipients,
		QuietHours: body.QuietHours, Enabled: body.Enabled, URL: body.Url,
	}
	if body.MinSeverity != nil {
		v := string(*body.MinSeverity)
		patch.MinSeverity = &v
	}
	secret := ""
	if body.Secret != nil {
		secret = *body.Secret
	}
	clear := body.ClearSecret != nil && *body.ClearSecret
	out, err := h.svc.UpdateChannel(c.Request.Context(), uuidStr(channelId), patch, secret, clear, h.actor(c))
	if err != nil {
		writeAdminErr(c, err)
		return
	}
	c.JSON(http.StatusOK, toGenChannel(out))
}

// DeleteNotificationChannel DELETE /alerting/channels/{channelId}
func (h *Handler) DeleteNotificationChannel(c *gin.Context, channelId openapi_types.UUID) {
	if err := h.svc.DeleteChannel(c.Request.Context(), uuidStr(channelId)); err != nil {
		writeAdminErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// TestNotificationChannel POST /alerting/channels/{channelId}/test
func (h *Handler) TestNotificationChannel(c *gin.Context, channelId openapi_types.UUID) {
	ok, status, err := h.svc.TestChannel(c.Request.Context(), uuidStr(channelId))
	if err != nil {
		writeAdminErr(c, err)
		return
	}
	out := apigen.ChannelTestResult{Ok: ok, Status: status}
	if !ok {
		out.Error = &status
	}
	c.JSON(http.StatusOK, out)
}

// ListNotificationDeliveries GET /alerting/deliveries
func (h *Handler) ListNotificationDeliveries(c *gin.Context, p apigen.ListNotificationDeliveriesParams) {
	f := DeliveryFilter{Limit: 50}
	if p.Limit != nil {
		f.Limit = *p.Limit
	}
	if p.ChannelId != nil {
		f.ChannelID = uuidStr(*p.ChannelId)
	}
	if p.Status != nil {
		f.Status = string(*p.Status)
	}
	list, total, err := h.svc.Deliveries(c.Request.Context(), f)
	if err != nil {
		writeAdminErr(c, err)
		return
	}
	items := make([]apigen.NotificationDelivery, 0, len(list))
	for _, d := range list {
		items = append(items, toGenDelivery(d))
	}
	c.JSON(http.StatusOK, apigen.NotificationDeliveryList{Items: items, Total: &total})
}
