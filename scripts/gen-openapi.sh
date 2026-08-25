#!/usr/bin/env bash
# 从 api/openapi 再生 api/gen（Go 接口 + TS 客户端）。
# --check 模式：重新生成到临时目录并与已提交的 api/gen 比对，不一致即退出非零（CI 门禁）。
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
spec="$root/api/openapi/openapi.yaml"
mode="${1:-gen}"

if [ ! -f "$spec" ]; then
  echo "skip gen: $spec 不存在（由任务 M0-004 创建最小 spec）"; exit 0
fi
command -v oapi-codegen >/dev/null 2>&1 || { echo "FAIL: oapi-codegen 未安装（go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest）"; exit 1; }

cfg="$root/api/oapi-codegen.yaml"
[ -f "$cfg" ] || { echo "FAIL: 缺少生成配置 $cfg（M0-004 定义）"; exit 1; }

if [ "$mode" = "--check" ]; then
  tmp=$(mktemp -d)
  cp -r "$root/api/gen" "$tmp/gen"
  (cd "$root" && oapi-codegen -config "$cfg" "$spec")
  if ! diff -r "$tmp/gen" "$root/api/gen" >/dev/null; then
    echo "FAIL: api/gen 与 spec 重新生成结果不一致——请运行 make gen 后提交"; exit 1
  fi
  rm -rf "$tmp"; echo "OK: api/gen 与 spec 一致"
else
  (cd "$root" && oapi-codegen -config "$cfg" "$spec")
  echo "OK: api/gen 已再生（TS 客户端生成在 M0-004 补充 web 侧管道）"
fi
