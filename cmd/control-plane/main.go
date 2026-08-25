// control-plane 入口：REST API + UI embed + 静态提示页（:8443）。
// 当前为 M0-004 后端腿：仅注册 OpenAPI 生成的路由；webui embed 由 M0-008 接入。
package main

import (
	"log"

	"github.com/gin-gonic/gin"

	apigen "github.com/xiaodaoi/ipam/api/gen/go"
	"github.com/xiaodaoi/ipam/internal/module/platform"
)

// version 构建期注入：-ldflags "-X main.version=x.y.z"
var version = "0.1.0-dev"

func main() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	h := platform.NewHandler(version)
	// spec servers.url=/api/v1 → 统一前缀注册
	apigen.RegisterHandlersWithOptions(r, h, apigen.GinServerOptions{BaseURL: "/api/v1"})

	addr := ":8443"
	log.Printf("control-plane %s listening on %s", version, addr)
	if err := r.Run(addr); err != nil {
		log.Fatal(err)
	}
}
