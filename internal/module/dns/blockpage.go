package dns

import (
	"encoding/json"
	"html"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// 拦截提示页（M6-023）：redirect 类封禁条目解析到 settings.blockPageIP，
// 浏览器随后以原始 Host 访问该 IP，控制面在此返回说明页（无需认证）。
//
// 页面完全自包含（内联样式，CSP 已允许 style-src 'unsafe-inline'），不依赖 WebUI 产物，
// 避免 DNS 拦截场景下前端路由/鉴权带来的不可用。

const defaultBlockTitle = "访问已被拦截"
const defaultBlockMessage = "该域名根据网络使用策略被禁止访问。如确有业务需要，请联系网络管理员申请开通。"

// BlockPageHandler 渲染拦截提示页；repo 为 nil 时使用默认文案。
func BlockPageHandler(repo SettingsRepo) gin.HandlerFunc {
	return func(c *gin.Context) {
		title, msg := defaultBlockTitle, defaultBlockMessage
		if repo != nil {
			if raw, ok, err := repo.Get(c.Request.Context(), "cache"); err == nil && ok && len(raw) > 0 {
				var s Settings
				if json.Unmarshal(raw, &s) == nil {
					if v := strings.TrimSpace(s.BlockPageTitle); v != "" {
						title = v
					}
					if v := strings.TrimSpace(s.BlockPageMessage); v != "" {
						msg = v
					}
				}
			}
		}
		host := c.Request.Host
		c.Header("Cache-Control", "no-store")
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(renderBlockPage(title, msg, host)))
	}
}

// renderBlockPage 生成提示页 HTML（所有动态值转义）。
func renderBlockPage(title, msg, host string) string {
	var sb strings.Builder
	sb.WriteString(`<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex,nofollow">
<title>`)
	sb.WriteString(html.EscapeString(title))
	sb.WriteString(`</title>
<style>
  :root { color-scheme: light dark; }
  * { box-sizing: border-box; }
  body { margin: 0; min-height: 100vh; display: flex; align-items: center; justify-content: center;
    padding: 24px; background: #f4f5f7; color: #1f2328;
    font: 15px/1.6 -apple-system, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif; }
  .card { width: 100%; max-width: 520px; background: #fff; border-radius: 14px; padding: 32px 28px;
    box-shadow: 0 10px 30px rgba(15, 23, 42, .10); }
  .mark { width: 46px; height: 46px; border-radius: 12px; display: flex; align-items: center;
    justify-content: center; background: #fee2e2; color: #b91c1c; font-size: 24px; font-weight: 700; }
  h1 { margin: 18px 0 10px; font-size: 20px; line-height: 1.35; }
  p { margin: 0 0 12px; color: #4b5563; }
  .host { padding: 10px 12px; background: #f8fafc; border: 1px solid #e5e7eb; border-radius: 8px;
    font-size: 13px; color: #334155; word-break: break-all; }
  .host code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
  .hint { margin-top: 16px; font-size: 13px; color: #6b7280; }
  @media (prefers-color-scheme: dark) {
    body { background: #111827; color: #e5e7eb; }
    .card { background: #1f2937; box-shadow: none; border: 1px solid #374151; }
    p { color: #cbd5e1; }
    .host { background: #111827; border-color: #374151; color: #e5e7eb; }
    .mark { background: #7f1d1d; color: #fecaca; }
  }
</style>
</head>
<body>
<main class="card">
  <div class="mark">!</div>
  <h1>`)
	sb.WriteString(html.EscapeString(title))
	sb.WriteString(`</h1>
  <p>`)
	sb.WriteString(html.EscapeString(msg))
	sb.WriteString(`</p>`)
	if host = strings.TrimSpace(host); host != "" {
		sb.WriteString(`
  <div class="host">访问目标：<code>`)
		sb.WriteString(html.EscapeString(host))
		sb.WriteString(`</code></div>`)
	}
	sb.WriteString(`
  <p class="hint">由 IPAM 统一 DNS 策略提供拦截与提示。</p>
</main>
</body>
</html>
`)
	return sb.String()
}
