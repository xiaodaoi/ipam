package logquery

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/xiaodaoi/ipam/internal/pkg/auditmeta"
)

// ActorProvider 从请求提取调用者身份（§12.3）。
// M5 JWT 接入前默认 system/anonymous；接入后由控制面注入 token 解析实现。
type ActorProvider = func(c *gin.Context) (string, string, string)

func DefaultActorProvider(*gin.Context) (ActorType, string, string) {
	return ActorSystem, "control-plane", ""
}

// BeforeLoader 在变更 handler 执行前加载“变更前实体”快照（M6-006）。
// resource 为归一化后的路由模板（已去 /api/v1 前缀）；loader 从 context/路径参数读取 ID。
// 返回 (nil, nil) 表示无可记录快照（如创建不存在旧实体）。
type BeforeLoader = func(ctx context.Context, c *gin.Context) (any, error)

// Recorder 变更类请求（POST/PATCH/DELETE/PUT）审计中间件。
// 通过 RegisterBefore 登记“变更前实体加载器”，把实体级 before 快照写入审计（脱敏）。
type Recorder struct {
	store   AuditStore
	extract actorExtractor
	mu      sync.RWMutex
	loaders map[string]BeforeLoader
}

// actorExtractor 内部身份提取签名（ActorType 已在类型层约束）。
type actorExtractor = func(c *gin.Context) (ActorType, string, string)

// NewAuditRecorder 构造审计记录器。provider 缺省为 DefaultActorProvider。
func NewAuditRecorder(store AuditStore, provider ...ActorProvider) *Recorder {
	extract := DefaultActorProvider
	if len(provider) > 0 && provider[0] != nil {
		raw := provider[0]
		extract = func(c *gin.Context) (ActorType, string, string) {
			at, actor, sub := raw(c)
			return ActorType(at), actor, sub
		}
	}
	return &Recorder{store: store, extract: extract, loaders: map[string]BeforeLoader{}}
}

// RegisterBefore 登记某资源模板的 before 加载器（resource 形如 "/subnets/:subnetId"）。
func (r *Recorder) RegisterBefore(resource string, fn BeforeLoader) *Recorder {
	if fn == nil {
		return r
	}
	r.mu.Lock()
	r.loaders[resource] = fn
	r.mu.Unlock()
	return r
}

func (r *Recorder) loader(resource string) BeforeLoader {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.loaders[resource]
}

// Middleware 返回 gin 中间件。
func (r *Recorder) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		mutating := isMutating(c.Request.Method)
		resource := strings.TrimPrefix(c.FullPath(), "/api/v1")
		var payload, before string
		if mutating {
			payload = capturePayload(c)
			// 变更前快照必须在 handler 执行前抓取（删除/更新会改变实体）。
			if fn := r.loader(resource); fn != nil && c.FullPath() != "" {
				before = snapshotEntity(c, fn)
			}
		}
		c.Next()

		switch c.Request.Method {
		case "POST", "PATCH", "DELETE", "PUT":
		default:
			return
		}
		if r.store == nil || c.FullPath() == "" {
			return
		}
		atype, actor, tokenSub := r.extract(c)
		action := ActionForMethod(c.Request.Method)
		if v := c.GetString(auditmeta.ActionKey); v != "" {
			action = v
		}
		if v := c.GetString(auditmeta.ActorKey); v != "" {
			actor = v
		}
		detail := c.GetString(auditmeta.DetailKey)
		if _, err := r.store.Append(c.Request.Context(), AuditEntry{
			ActorType: atype, Actor: actor, TokenSub: tokenSub,
			Method:   c.Request.Method,
			Path:     c.Request.URL.Path,
			Action:   action,
			Resource: resource,
			Status:   c.Writer.Status(),
			Detail:   detail,
			Payload:  payload,
			Before:   before,
		}); err != nil {
			log.Printf("[audit] append %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
		}
	}
}

// snapshotEntity 调用 loading 加载实体并返回脱敏后的 JSON（≤8KB）；失败/超限返回空串。
func snapshotEntity(c *gin.Context, fn BeforeLoader) string {
	v, err := fn(c.Request.Context(), c)
	if err != nil || v == nil {
		return ""
	}
	raw, err := json.Marshal(v)
	if err != nil || len(raw) == 0 {
		return ""
	}
	var anyv any
	if err := json.Unmarshal(raw, &anyv); err != nil {
		return ""
	}
	redactSensitive(anyv)
	out, err := json.Marshal(anyv)
	if err != nil || len(out) > 8*1024 {
		return ""
	}
	return string(out)
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
			if isSensitiveKey(k) {
				switch val.(type) {
				case string, float64, int, int64, json.Number:
					// 密钥/口令类值一律打码（fail-closed）。
					t[k] = "***"
					continue
				default:
					// 布尔策略开关（如 forcePasswordChange）等非文本值不视为机密，继续向下处理。
				}
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

// isSensitiveKey 判断字段名是否属于需脱敏的机密类。
func isSensitiveKey(k string) bool {
	lk := strings.ToLower(k)
	return strings.Contains(lk, "password") || strings.Contains(lk, "token") ||
		strings.Contains(lk, "secret") || strings.Contains(lk, "license") ||
		strings.Contains(lk, "private") || strings.Contains(lk, "authorization") ||
		strings.Contains(lk, "cookie")
}
