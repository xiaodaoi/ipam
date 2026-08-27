package logquery

import (
	"bytes"
	"encoding/csv"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/pkg/problem"
)

// Handler 实现 apigen.ServerInterface 中 logs 域端点（§13.4 日志中心检索）。
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// ListLogs GET /logs
func (h *Handler) ListLogs(c *gin.Context, params apigen.ListLogsParams) {
	f := LogFilter{
		From:     params.From,
		Cursor:   derefStr(params.Cursor),
		PageSize: derefInt(params.PageSize, DefaultPage),
	}
	if params.To != nil {
		f.To = *params.To
	}
	if params.Type != nil {
		f.Type = string(*params.Type)
	}
	if params.Mac != nil {
		f.MAC = string(*params.Mac)
	}
	if params.Ip != nil {
		f.IP = string(*params.Ip)
	}
	if params.Domain != nil {
		f.Domain = string(*params.Domain)
	}
	if params.Action != nil {
		f.Action = string(*params.Action)
	}
	if params.OrgId != nil {
		f.OrgID = params.OrgId.String()
	}
	page, err := h.svc.Query(c.Request.Context(), f)
	if err != nil {
		writeLogErr(c, err)
		return
	}
	items := make([]apigen.LogRow, 0, len(page.Items))
	for _, r := range page.Items {
		items = append(items, toGenLogRow(r))
	}
	var next *string
	if page.NextCursor != "" {
		next = &page.NextCursor
	}
	c.JSON(http.StatusOK, apigen.LogPage{Items: items, NextCursor: next, Total: &page.Total})
}

// ListLogTop GET /logs/top
func (h *Handler) ListLogTop(c *gin.Context, params apigen.ListLogTopParams) {
	q := TopQuery{From: params.From, By: "domain", Limit: DefaultLimit}
	if params.To != nil {
		q.To = *params.To
	}
	if params.Type != nil {
		q.Type = string(*params.Type)
	}
	if params.Ip != nil {
		q.IP = string(*params.Ip)
	}
	if params.Action != nil {
		q.Action = string(*params.Action)
	}
	if params.OrgId != nil {
		q.OrgID = params.OrgId.String()
	}
	if params.By != nil {
		q.By = string(*params.By)
	}
	if params.Limit != nil {
		q.Limit = *params.Limit
	}
	entries, total, err := h.svc.Top(c.Request.Context(), q)
	if err != nil {
		writeLogErr(c, err)
		return
	}
	items := make([]apigen.TopEntry, 0, len(entries))
	for _, e := range entries {
		items = append(items, apigen.TopEntry{Key: e.Key, Count: e.Count})
	}
	c.JSON(http.StatusOK, apigen.TopList{Items: items, Total: &total})
}

// GetLogQps GET /logs/qps
func (h *Handler) GetLogQps(c *gin.Context, params apigen.GetLogQpsParams) {
	q := QpsQuery{From: params.From, IntervalSec: DefaultBucket}
	if params.To != nil {
		q.To = *params.To
	}
	if params.Type != nil {
		q.Type = string(*params.Type)
	}
	if params.Action != nil {
		q.Action = string(*params.Action)
	}
	if params.OrgId != nil {
		q.OrgID = params.OrgId.String()
	}
	if params.IntervalSec != nil {
		q.IntervalSec = *params.IntervalSec
	}
	points, err := h.svc.Qps(c.Request.Context(), q)
	if err != nil {
		writeLogErr(c, err)
		return
	}
	genPoints := make([]apigen.QpsPoint, 0, len(points))
	for _, p := range points {
		genPoints = append(genPoints, apigen.QpsPoint{Ts: p.TS, Count: p.Count})
	}
	c.JSON(http.StatusOK, apigen.QpsSeries{Points: genPoints})
}

// ExportLogs GET /logs/export（CSV 单次转储，ts 倒序）
func (h *Handler) ExportLogs(c *gin.Context, params apigen.ExportLogsParams) {
	f := LogFilter{From: params.From, PageSize: derefInt(params.ExportLimit, 10000)}
	if params.To != nil {
		f.To = *params.To
	}
	if params.Type != nil {
		f.Type = string(*params.Type)
	}
	if params.Mac != nil {
		f.MAC = string(*params.Mac)
	}
	if params.Ip != nil {
		f.IP = string(*params.Ip)
	}
	if params.Domain != nil {
		f.Domain = string(*params.Domain)
	}
	if params.Action != nil {
		f.Action = string(*params.Action)
	}
	if params.OrgId != nil {
		f.OrgID = params.OrgId.String()
	}
	page, err := h.svc.Query(c.Request.Context(), f)
	if err != nil {
		writeLogErr(c, err)
		return
	}
	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF") // UTF-8 BOM（Excel 兼容）
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"ts", "type", "severity", "client_mac", "client_ip", "sip", "domain", "rcode", "action", "category", "detail"})
	for _, r := range page.Items {
		_ = w.Write([]string{
			r.TS.UTC().Format(time.RFC3339Nano), r.Type, r.Severity, r.ClientMAC,
			r.ClientIP, r.SIP, r.Domain, r.Rcode, r.Action, r.Category, r.Detail,
		})
	}
	w.Flush()
	c.Header("Content-Disposition", `attachment; filename="logs-`+params.From.UTC().Format("20060102")+`.csv"`)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", buf.Bytes())
}

func toGenLogRow(r LogRow) apigen.LogRow {
	return apigen.LogRow{
		Ts:        r.TS,
		Type:      apigen.LogRowType(r.Type),
		Severity:  optStr(r.Severity),
		ClientMac: optStr(r.ClientMAC),
		ClientIp:  optStr(r.ClientIP),
		Sip:       optStr(r.SIP),
		Domain:    optStr(r.Domain),
		Rcode:     optStr(r.Rcode),
		Action:    optStr(r.Action),
		Category:  optStr(r.Category),
		Detail:    optStr(r.Detail),
	}
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefInt(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

// writeLogErr 错误 → RFC 9457（400 过滤参数 / 404 组织 / 503 存储不可达）。
func writeLogErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrMissingFrom), errors.Is(err, ErrInvalidCursor), errors.Is(err, ErrWindowTooLarge):
		problem.Write(c, http.StatusBadRequest, "https://ipam.local/problems/bad-request", "INVALID_FILTER", err.Error())
	case errors.Is(err, ErrOrgNotFound):
		problem.Write(c, http.StatusNotFound, "https://ipam.local/problems/not-found", "ORG_NOT_FOUND", err.Error())
	default:
		problem.Write(c, http.StatusServiceUnavailable, "https://ipam.local/problems/log-store-down", "CH_DOWN", "日志存储暂不可达")
	}
}
