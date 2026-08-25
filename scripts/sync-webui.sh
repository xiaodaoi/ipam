#!/usr/bin/env bash
# 同步前端构建产物到 go:embed 目录（§13.3）。
# 用法: sync-webui.sh [--build]  （--build 时先执行 pnpm run build:ipam）
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
src="$root/web/apps/web-ipam/dist"
dst="$root/cmd/control-plane/webui/dist"

if [ "${1:-}" = "--build" ]; then
  (cd "$root/web" && pnpm run build:ipam)
fi
[ -f "$src/index.html" ] || { echo "FAIL: $src/index.html 不存在，先执行 --build"; exit 1; }

rm -rf "$dst"
mkdir -p "$dst"
cp -r "$src/." "$dst/"
touch "$dst/.gitkeep"      # 保留被跟踪占位（fresh clone 可编译），内容为空无副作用
echo "OK: webui synced ($(du -sh "$dst" | cut -f1))"
