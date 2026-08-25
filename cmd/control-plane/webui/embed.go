// Package webui 承载前端构建产物的 go:embed（§13.3 单二进制交付）。
// dist 由 scripts/sync-webui.sh 从 web/apps/web-ipam/dist 同步；本包禁止手改 dist 内容。
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// FS 返回以 dist 为根的只读文件系统。
func FS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
