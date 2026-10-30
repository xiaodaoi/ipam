#!/usr/bin/env bash
# 生成离线交付包（M6-012）：镜像 tar + compose/config/db/docs + 校验和。
# 用法: scripts/package-offline.sh [输出目录，默认 dist/offline]
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
out="${1:-$root/dist/offline}"
mkdir -p "$out"

cd "$root/deploy/compose"
mapfile -t images < <(docker compose config --images | sort -u)
echo "镜像: ${#images[@]}"
for img in "${images[@]}"; do
  safe="$(echo "$img" | tr '/:@' '___')"
  echo "→ save $img"
  docker save "$img" | gzip -1 > "$out/image-${safe}.tar.gz"
done

echo "→ 打包配置/部署/文档"
tar -C "$root" -czf "$out/ipam-bundle.tar.gz" \
  deploy config db observability docs README.md LICENSES.md Makefile scripts
(cd "$out" && sha256sum ./*.tar.gz > SHA256SUMS)
echo "OK: $out"
echo "离线安装：解压 ipam-bundle.tar.gz 后进入 deploy/compose，加载 image-*.tar.gz 并执行 install.sh"
