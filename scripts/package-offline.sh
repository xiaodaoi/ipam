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
# 密钥红线（AGENTS.md）：.env 与运行时证书/私钥不得进入交付包。
# install.sh 在目标机用 .env.example 生成 .env，故排除 .env 不影响安装（M6-029）。
bundle="$out/ipam-bundle.tar.gz"
tar -C "$root" \
  --exclude='deploy/compose/.env' \
  --exclude='deploy/compose/certs' \
  --exclude='deploy/compose/certs/*' \
  --exclude='*.pem' \
  --exclude='*.key' \
  --exclude='*.crt' \
  --exclude='*.p12' \
  --exclude='*.pfx' \
  -czf "$bundle" \
  deploy config db observability docs README.md LICENSES.md Makefile scripts

# 打包后自检：命中敏感文件即失败，避免本机口令/证书随交付包外发。
bash "$root/scripts/check-bundle-secrets.sh" "$bundle"
(cd "$out" && sha256sum ./*.tar.gz > SHA256SUMS)
echo "OK: $out"
echo "离线安装：解压 ipam-bundle.tar.gz 后进入 deploy/compose，加载 image-*.tar.gz 并执行 install.sh"
