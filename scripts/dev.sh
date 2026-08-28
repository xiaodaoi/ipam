#!/usr/bin/env bash
# 调试循环主通道（§13.3）：宿主机跑 control-plane，容器只跑依赖。
# 免镜像构建：Go 秒级重编 + 前端走磁盘 dist（IPAM_WEBUI_DIR）或 Vite 热更（make dev-web）。
# 用法: scripts/dev.sh [--web]   --web 同时起 Vite dev server（前端 HMR）
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

want_web=false
[ "${1:-}" = "--web" ] && want_web=true

# 0) 容器版 control-plane 与宿主进程抢 8443 → 停掉（PG/CH/daemon 等依赖保留）
#    调试结束恢复：docker compose -f deploy/compose/compose.yaml start control-plane
docker compose -f deploy/compose/compose.yaml stop control-plane >/dev/null 2>&1 || true

# 1) 依赖容器（镜像已建则零构建；PG/CH 端口仅发布到 127.0.0.1）
docker compose -f deploy/compose/compose.yaml up -d --wait postgresql clickhouse

# 2) 凭据以 compose .env 为准（POSTGRES_PASSWORD 为必填项）
if [ -f deploy/compose/.env ]; then
  set -a; . deploy/compose/.env; set +a
fi

PG_USER="${POSTGRES_USER:-ipam}"
PG_PASS="${POSTGRES_PASSWORD:?POSTGRES_PASSWORD 未设置（deploy/compose/.env）}"
PG_DB="${POSTGRES_DB:-ipam}"
CH_DB="${CLICKHOUSE_DB:-ipam}"
CH_USER="${CLICKHOUSE_USER:-ipam}"
CH_PASS="${CLICKHOUSE_PASSWORD:-}"

export IPAM_HTTP_ADDR="${IPAM_HTTP_ADDR:-127.0.0.1:8443}"
export IPAM_DB_DSN="postgres://${PG_USER}:${PG_PASS}@127.0.0.1:${IPAM_PG_PORT:-5432}/${PG_DB}?sslmode=disable"
export IPAM_CH_ADDR="127.0.0.1:${IPAM_CH_PORT:-9000}"
export IPAM_CH_DB="$CH_DB" IPAM_CH_USER="$CH_USER" IPAM_CH_PASSWORD="$CH_PASS"
# 前端走磁盘 dist（改前端后 pnpm build:ipam 即生效，无需 go 重编/镜像）；Vite 模式下也可留空走 embed
[ -d web/apps/web-ipam/dist ] && export IPAM_WEBUI_DIR="$root/web/apps/web-ipam/dist"
# kea/unbound 宿主不存在 → 引擎软失败（探活灯 down），不影响业务 API 调试
unset IPAM_KEA_API || true

mkdir -p bin
echo "── go build (秒级) ──"
go build -o bin/dev-control-plane ./cmd/control-plane

if $want_web; then
  echo "── 控制面 :8443 + Vite dev（前端 HMR，/api 代理 → 8443）──"
  (cd web && pnpm --filter @vben/web-ipam run dev) &
  trap 'kill $(jobs -p) 2>/dev/null' EXIT
fi

echo "── 宿主 control-plane 启动（Ctrl-C 退出；改 Go 代码后重跑本脚本）──"
exec bin/dev-control-plane
