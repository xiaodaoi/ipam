#!/usr/bin/env bash
# 分部执行器：按工具链就绪情况执行 build/test/lint，未就绪部分明确跳过。
root="$(cd "$(dirname "$0")/.." && pwd)"
# 用法: make-part.sh <build|test|lint>
set -u
part="${1:?usage: make-part.sh <build|test|lint>}"
rc=0

run_go() {
  if ! command -v go >/dev/null 2>&1; then echo "skip go: 未安装"; return 0; fi
  if [ ! -f go.mod ]; then echo "skip go: go.mod 未就绪"; return 0; fi
  "$@"
}

case "$part" in
  build)
    run_go go build -o bin/ ./cmd/... || rc=1
    if [ -f web/package.json ]; then
      # 离线图标集合先行再生（M6-018），保证产物随源码最新；node_modules 未就绪则跳过。
      if command -v node >/dev/null 2>&1 && [ -d web/node_modules ]; then
        node "$root/scripts/gen-offline-icons.mjs" || rc=1
      else
        echo "skip gen-icons: node 或 web/node_modules 未就绪"
      fi
      command -v pnpm >/dev/null 2>&1 && (cd web && pnpm run build:ipam) && bash "$root/scripts/sync-webui.sh" || { echo "skip web: pnpm 未启用(corepack enable)"; [ -f web/apps/web-ipam ] && rc=1; }
    else
      echo "skip web: Vben 底座未引入(M0-007)"
    fi
    ;;
  test)
    run_go go test ./... || rc=1
    if [ -f web/package.json ] && command -v pnpm >/dev/null 2>&1; then
      (cd web && pnpm run test) || rc=1
    else
      echo "skip web-test"
    fi
    # 离线图标集合与源码一致性门禁（M6-018）。
    if command -v node >/dev/null 2>&1 && [ -d web/node_modules ]; then
      node "$root/scripts/gen-offline-icons.mjs" --check || rc=1
    else
      echo "skip gen-icons-check: node 或 web/node_modules 未就绪"
    fi
    ;;
  lint)
    if command -v golangci-lint >/dev/null 2>&1 && [ -f .golangci.yml ]; then
      golangci-lint run || rc=1
    else
      echo "skip go-lint: 工具或 .golangci.yml 未就绪"
    fi
    if [ -f web/package.json ] && command -v pnpm >/dev/null 2>&1; then
      (cd web && pnpm run lint) || rc=1
    else
      echo "skip web-lint: 底座未引入"
    fi
    ;;
  *) echo "unknown part: $part"; exit 2 ;;
esac
exit $rc
