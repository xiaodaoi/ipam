package logexport

import (
	"net/http"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

type LogExportHandler struct{ svc *Service }

func NewLogExportHandler(svc *Service) *LogExportHandler { return &LogExportHandler{svc: svc} }

func toGen(s Settings) apigen.LogExportSettings {
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
	return out
}

func (h *LogExportHandler) GetLogExportSettings(c *gin.Context) {
	s, err := h.svc.Get(c.Request.Context())
	if err != nil {
		problem.Write(c, 500, "https://ipam.local/problems/internal", "DB_ERROR", err.Error())
		return
	}
	c.JSON(http.StatusOK, toGen(s))
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
	c.JSON(http.StatusOK, toGen(saved))
}
