package logexport

import (
	"net/http"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

type LogExportHandler struct{ svc *Service }

func NewLogExportHandler(svc *Service) *LogExportHandler { return &LogExportHandler{svc: svc} }

func toGen(s Settings, st Status) apigen.LogExportSettings {
	mode := apigen.LogExportSettingsSyslogMode(s.SyslogMode)
	if mode == "" {
		mode = "tcp"
	}
	out := apigen.LogExportSettings{
		SyslogEnabled: s.SyslogEnabled, SyslogMode: &mode,
		KafkaEnabled: s.KafkaEnabled, WebhookEnabled: s.WebhookEnabled,
		SamplingMode: apigen.LogExportSettingsSamplingMode(s.SamplingMode),
		SampleRate:   s.SampleRate,
	}
	if s.SyslogAddress != "" {
		out.SyslogAddress = &s.SyslogAddress
	}
	if s.KafkaBrokers != "" {
		out.KafkaBrokers = &s.KafkaBrokers
	}
	if s.KafkaTopic != "" {
		out.KafkaTopic = &s.KafkaTopic
	}
	if s.WebhookURL != "" {
		out.WebhookUrl = &s.WebhookURL
	}
	if !s.UpdatedAt.IsZero() {
		out.UpdatedAt = &s.UpdatedAt
	}
	if st.EffectiveSamplingMode != "" {
		mode := apigen.LogExportSettingsEffectiveSamplingMode(st.EffectiveSamplingMode)
		out.EffectiveSamplingMode = &mode
	}
	ingest := st.EffectiveIngestSampleRate
	if ingest < 1 {
		ingest = 1
	}
	out.IngestSampleRate = &ingest
	degraded := st.Degraded
	out.Degraded = &degraded
	if st.DiskUsedPercent > 0 {
		pct := float32(st.DiskUsedPercent)
		out.DiskUsedPercent = &pct
	}
	if !st.DegradedAt.IsZero() {
		at := st.DegradedAt
		out.DegradedAt = &at
	}
	return out
}

func (h *LogExportHandler) GetLogExportSettings(c *gin.Context) {
	s, err := h.svc.Get(c.Request.Context())
	if err != nil {
		problem.Write(c, 500, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.JSON(http.StatusOK, toGen(s, h.svc.Status()))
}

// GetLogSampling GET /logs/sampling：内部入库抽样率与磁盘降级状态（M6-045）。
// 仅返回采样元数据，不含外发地址等配置，供仪表盘/日志概览提示“KPI 为抽样下界”。
func (h *LogExportHandler) GetLogSampling(c *gin.Context) {
	st := h.svc.Status()
	rate := st.EffectiveIngestSampleRate
	if rate < 1 {
		rate = 1
	}
	out := apigen.LogSamplingStatus{Degraded: st.Degraded, IngestSampleRate: rate}
	if st.DiskUsedPercent > 0 {
		pct := float32(st.DiskUsedPercent)
		out.DiskUsedPercent = &pct
	}
	if !st.DegradedAt.IsZero() {
		at := st.DegradedAt
		out.DegradedAt = &at
	}
	c.JSON(http.StatusOK, out)
}

func (h *LogExportHandler) UpdateLogExportSettings(c *gin.Context) {
	var body apigen.LogExportSettings
	if err := c.ShouldBindJSON(&body); err != nil {
		problem.Write(c, 400, "https://ipam.local/problems/bad-request", "BAD_REQUEST", err.Error())
		return
	}
	st := Settings{
		SyslogEnabled: body.SyslogEnabled,
		KafkaEnabled:  body.KafkaEnabled, WebhookEnabled: body.WebhookEnabled,
		SamplingMode: string(body.SamplingMode), SampleRate: body.SampleRate,
	}
	if body.SyslogMode != nil {
		st.SyslogMode = string(*body.SyslogMode)
	}
	if body.SyslogAddress != nil {
		st.SyslogAddress = *body.SyslogAddress
	}
	if body.KafkaBrokers != nil {
		st.KafkaBrokers = *body.KafkaBrokers
	}
	if body.KafkaTopic != nil {
		st.KafkaTopic = *body.KafkaTopic
	}
	if body.WebhookUrl != nil {
		st.WebhookURL = *body.WebhookUrl
	}
	saved, err := h.svc.Save(c.Request.Context(), st)
	if err != nil {
		problem.Write(c, 400, "https://ipam.local/problems/log-export", "LOG_EXPORT_INVALID", err.Error())
		return
	}
	c.JSON(http.StatusOK, toGen(saved, h.svc.Status()))
}
