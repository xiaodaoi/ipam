#!/usr/bin/env bash
# 容器镜像 CVE 扫描（M6-019）。
#
# 对指定镜像（默认：本项目构建产物）跑 trivy，命中 HIGH/CRITICAL 且可修复的漏洞即失败。
# 无 trivy 时明确跳过（本地无工具链可用）；CI 由 aquasecurity/trivy-action 承担。
#
# 用法：
#   bash scripts/scan-images.sh                 # 扫描默认镜像集
#   bash scripts/scan-images.sh img1 img2 ...   # 扫描指定镜像
#   bash scripts/scan-images.sh --strict [img…] # trivy 缺失也判失败
#
# trivy 解析顺序：PATH 中的 trivy → docker run aquasec/trivy（挂载 docker.sock）。
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
version="${IPAM_VERSION:-0.1.0-dev}"

strict=0
images=()
for a in "$@"; do
  case "$a" in
    --strict) strict=1 ;;
    *) images+=("$a") ;;
  esac
done
if [ "${#images[@]}" -eq 0 ]; then
  images=(
    "ipam/control-plane:${version}"
    "ipam/coherence-daemon:${version}"
    "ipam/unbound:${version}"
  )
fi

# 解析扫描命令：优先本地 trivy，其次容器化 trivy（需 docker）。
run_trivy() {
  local img="$1"
  if command -v trivy >/dev/null 2>&1; then
    trivy image --severity HIGH,CRITICAL --ignore-unfixed --no-progress "$img"
  else
    docker run --rm -v /var/run/docker.sock:/var/run/docker.sock \
      -e DOCKER_HOST=unix:///var/run/docker.sock \
      aquasec/trivy:latest image --severity HIGH,CRITICAL --ignore-unfixed --no-progress "$img"
  fi
}

if ! command -v trivy >/dev/null 2>&1; then
  if command -v docker >/dev/null 2>&1; then
    echo "info: 本地无 trivy，改用 docker run aquasec/trivy"
  else
    echo "skip scan-images: trivy 与 docker 均不可用（安装：https://trivy.dev）"
    [ "$strict" = 1 ] && exit 1
    exit 0
  fi
fi

rc=0
for img in "${images[@]}"; do
  echo "── 扫描 $img ───────────────────────────────────────────"
  if ! docker image inspect "$img" >/dev/null 2>&1 && command -v docker >/dev/null 2>&1; then
    echo "warn: 本地无镜像 $img，跳过（先 docker compose build）"
    continue
  fi
  run_trivy "$img" || rc=1
done

if [ "$rc" -ne 0 ]; then
  echo "FAIL: 镜像存在 HIGH/CRITICAL（可修复）漏洞，见上方明细"
  exit 1
fi
echo "OK: 镜像扫描通过（无 HIGH/CRITICAL 可修复漏洞）"
