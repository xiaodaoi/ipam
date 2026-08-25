package main

import (
	"io/fs"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/cmd/control-plane/webui"
	"github.com/xiaodaoi/ipam/internal/module/platform"
)

// newEngine 装配完整路由：/api/v1 业务路由 + webui embed + SPA fallback（§13.3）。
func newEngine(version string) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	h := platform.NewHandler(version)
	// spec servers.url=/api/v1 → 统一前缀注册
	apigen.RegisterHandlersWithOptions(r, h, apigen.GinServerOptions{BaseURL: "/api/v1"})

	dist, err := webui.FS()
	if err != nil {
		log.Fatalf("webui fs: %v", err)
	}
	serveStatic := func(c *gin.Context, name string) {
		data, rerr := fs.ReadFile(dist, name)
		if rerr != nil {
			platform.WriteProblem(c, http.StatusInternalServerError,
				"https://ipam.local/problems/internal", "WEBUI_ASSET_MISSING", "静态资源读取失败")
			return
		}
		ct := mime.TypeByExtension(filepath.Ext(name))
		if ct == "" {
			ct = "application/octet-stream"
		}
		c.Data(http.StatusOK, ct, data)
	}
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api/") {
			// API 未匹配路由统一返回 RFC 9457，便于机器判读（§12.2）
			platform.WriteProblem(c, http.StatusNotFound,
				"https://ipam.local/problems/not-found", "API_ROUTE_NOT_FOUND", "未知 API 路由")
			return
		}
		// 静态资源命中则回文件；否则回 index.html（history 路由 SPA fallback）
		target := strings.TrimPrefix(path, "/")
		if target == "" {
			target = "index.html"
		}
		if _, oerr := fs.Stat(dist, target); oerr == nil && !strings.HasSuffix(target, "/") {
			serveStatic(c, target)
			return
		}
		serveStatic(c, "index.html")
	})
	return r
}

// version 构建期注入：-ldflags "-X main.version=x.y.z"
var version = "0.1.0-dev"

func main() {
	addr := ":8443"
	log.Printf("control-plane %s listening on %s", version, addr)
	if err := newEngine(version).Run(addr); err != nil {
		log.Fatal(err)
	}
}
