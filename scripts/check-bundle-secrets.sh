#!/usr/bin/env bash
# 交付包密钥红线自检（M6-029）：命中 .env（.env.example 除外）/certs/证书私钥即失败。
# 用法: scripts/check-bundle-secrets.sh <bundle.tar.gz>
set -euo pipefail

bundle="${1:?用法: check-bundle-secrets.sh <bundle.tar.gz>}"
[ -f "$bundle" ] || { echo "FAIL: 交付包不存在: $bundle" >&2; exit 1; }

hits="$(tar -tzf "$bundle" \
  | grep -E '(^|/)\.env$|(^|/)\.env\.|/certs/|\.(pem|key|crt|p12|pfx)$' \
  | grep -v '\.env\.example$' || true)"

if [ -n "$hits" ]; then
  echo "FAIL: 交付包含 .env/证书/私钥（密钥红线，禁止外发）：" >&2
  echo "$hits" | head -20 >&2
  exit 1
fi
echo "OK: 交付包不含 .env/证书/私钥"
