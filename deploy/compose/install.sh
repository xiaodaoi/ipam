#!/usr/bin/env bash
# IPAM compose 一键部署 + 预检（§7/§9：裸 Ubuntu22.04 ≤30min 出服务）
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"

fail() { echo "✗ $1"; exit 1; }
ok()   { echo "✓ $1"; }

echo "── IPAM 部署预检 ──"
command -v docker >/dev/null || fail "未安装 docker"
docker compose version >/dev/null 2>&1 || fail "docker compose v2 不可用"
ok "docker/compose"

command -v ss >/dev/null && {
  for p in ${IPAM_PORT:-8443}; do
    ss -lun "( sport = :$p )" | grep -q "$p" && fail "端口 $p 已被占用" || ok "端口 $p 空闲"
  done
}

[ -f "$here/.env" ] || { cp "$here/.env.example" "$here/.env"; echo "→ 已生成 .env（请修改 POSTGRES_PASSWORD 后重跑）"; }
grep -q "change-me" "$here/.env" && fail ".env 中 POSTGRES_PASSWORD 仍为默认值，请先修改"

cd "$here"
docker compose up -d --wait
sleep 2
curl -fsS "http://127.0.0.1:${IPAM_PORT:-8443}/api/v1/system/info" | grep -q ipam-control-plane \
  && ok "服务就绪：https://127.0.0.1:${IPAM_PORT:-8443}（当前 HTTP，TLS 于 M5 接入）" \
  || fail "冒烟失败，查看 docker compose logs control-plane"
