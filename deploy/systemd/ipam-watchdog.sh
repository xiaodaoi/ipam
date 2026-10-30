#!/usr/bin/env bash
# 单机韧性看门狗（M6-003A）：健康失败/容器 unhealthy 时自动拉起/重启。
set -uo pipefail
root="${IPAM_DIR:-__IPAM_DIR__}"
port="${IPAM_PORT:-8443}"
cd "$root/deploy/compose" || exit 0

if ! curl -kfsS --max-time 5 "https://127.0.0.1:${port}/healthz" >/dev/null 2>&1; then
  echo "[watchdog] healthz failed, compose up -d"
  docker compose up -d --wait || true
fi

for id in $(docker compose ps -q 2>/dev/null); do
  health="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{end}}' "$id" 2>/dev/null || true)"
  if [ "$health" = "unhealthy" ]; then
    name="$(docker inspect -f '{{.Name}}' "$id" | sed 's#^/##')"
    echo "[watchdog] restart unhealthy $name"
    docker restart "$id" >/dev/null 2>&1 || true
  fi
done
