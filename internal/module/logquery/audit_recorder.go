package logquery

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/xiaodaoi/ipam/internal/pkg/auditmeta"
)

// ActorProvider 从请求提取调用者身份（§12.3）。
// M5 JWT 接入前默认 system/anonymous；接入后由控制面注入 token 解析实现。
type ActorProvider = func(c *gin.Context) (string, string, string)

func DefaultActorProvider(*gin.Context) (ActorType, string, string) {
	return ActorSystem, "control-plane", ""
}

// NewAuditRecorder 变更类请求（POST/PATCH/DELETE）审计中间件。
// resource 取路由模板（路径参数归一化），status 为响应码；匿名路由（FullPath 空）不入库。
func NewAuditRecorder(store AuditStore, provider ...ActorProvider) gin.HandlerFunc {
	extract := DefaultActorProvider
	if len(provider) > 0 && provider[0] != nil {
		raw := provider[0]
		extract = func(c *gin.Context) (ActorType, string, string) {
			at, actor, sub := raw(c)
			return ActorType(at), actor, sub
		}
	}
	return func(c *gin.Context) {
		var payload string
		if isMutating(c.Request.Method) {
			payload = capturePayload(c)
		}
		c.Next()

		switch c.Request.Method {
		case "POST", "PATCH", "DELETE":
		default:
			return
		}
		if store == nil || c.FullPath() == "" {
			return
		}
		atype, actor, tokenSub := extract(c)
		resource := strings.TrimPrefix(c.FullPath(), "/api/v1")
		action := ActionForMethod(c.Request.Method)
		if v := c.GetString(auditmeta.ActionKey); v != "" {
			action = v
		}
		if v := c.GetString(auditmeta.ActorKey); v != "" {
			actor = v
		}
		detail := c.GetString(auditmeta.DetailKey)
		if _, err := store.Append(c.Request.Context(), AuditEntry{
			ActorType: atype, Actor: actor, TokenSub: tokenSub,
			Method:   c.Request.Method,
			Path:     c.Request.URL.Path,
			Action:   action,
			Resource: resource,
			Status:   c.Writer.Status(),
			Detail:   detail,
			Payload:  payload,
		}); err != nil {
			log.Printf("[audit] append %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
		}
	}
}

func isMutating(method string) bool {
	return method == "POST" || method == "PATCH" || method == "DELETE" || method == "PUT"
}

// capturePayload 读取并恢复请求体，返回脱敏后的 JSON（≤8KB，仅 JSON）。
func capturePayload(c *gin.Context) string {
	if c.Request.Body == nil {
		return ""
	}
	const maxBody = 8 * 1024
	orig := c.Request.Body
	buf, err := io.ReadAll(io.LimitReader(orig, maxBody+1))
	c.Request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(buf), orig))
	if err != nil || len(buf) == 0 || len(buf) > maxBody {
		return ""
	}
	var v any
	if err := json.Unmarshal(buf, &v); err != nil {
		return ""
	}
	redactSensitive(v)
	out, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(out)
}

func redactSensitive(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			lk := strings.ToLower(k)
			if strings.Contains(lk, "password") || strings.Contains(lk, "token") ||
				strings.Contains(lk, "secret") || strings.Contains(lk, "license") ||
				strings.Contains(lk, "private") || strings.Contains(lk, "authorization") ||
				strings.Contains(lk, "cookie") {
				t[k] = "***"
				continue
			}
			t[k] = redactSensitive(val)
		}
	case []any:
		for i := range t {
			t[i] = redactSensitive(t[i])
		}
	}
	return v
}
