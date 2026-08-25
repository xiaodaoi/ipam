#!/usr/bin/env bash
# 工具链自检：报告各工具可用性；--strict 时缺失硬依赖则退出非零（供 CI 用）。
set -u
strict=0
[ "${1:-}" = "--strict" ] && strict=1

declare -A TOOLS=(
  [git]="版本控制"
  [go]="Go 工具链(>=1.22)｜后端构建/测试"
  [golangci-lint]="Go 静态检查"
  [node]="Node.js(>=20)｜前端构建"
  [corepack]="pnpm 启用器(node 自带)"
  [pnpm]="前端包管理"
  [docker]="镜像/compose 冒烟"
  [oapi-codegen]="OpenAPI→Go 代码生成(M0-004 起)"
  [spectral]="spec lint 门禁(M0-005 起)"
)

miss_hard=0
printf "%-16s %-8s %s\n" "TOOL" "STATUS" "用途"
for t in git go golangci-lint node corepack pnpm docker oapi-codegen spectral; do
  if command -v "$t" >/dev/null 2>&1; then
    printf "%-16s \033[32m%-8s\033[0m %s\n" "$t" OK "${TOOLS[$t]}"
  else
    printf "%-16s \033[33m%-8s\033[0m %s\n" "$t" MISSING "${TOOLS[$t]}"
    case "$t" in go|node|pnpm) miss_hard=$((miss_hard+1));; esac
  fi
done

# 版本下限抽查
command -v go >/dev/null 2>&1 && {
  v=$(go version | awk '{print $3}' | tr -d 'go')
  awk -V a="$v" 'BEGIN{exit (a>=1.22)?0:1}' || { echo "!! go $v < 1.22"; miss_hard=$((miss_hard+1)); }
}

echo "---"
if [ "$strict" -eq 1 ] && [ "$miss_hard" -gt 0 ]; then
  echo "STRICT: ${miss_hard} 个硬依赖缺失"; exit 1
fi
echo "doctor: 完成（缺失项按任务卡需要时安装；pnpm 可经 corepack enable 启用）"
