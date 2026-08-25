#!/usr/bin/env bash
# 前端产物零外链断言（§13.3：内网离线可用）。
# 检测"会被浏览器加载的外部资源"：<link|script|img 的绝对 http(s) 引用、CSS url()/@import 外链。
# JS 代码内的字符串 URL（如开源库官网链接）不构成加载行为，仅提示不计入失败。
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
dist="$root/cmd/control-plane/webui/dist"
[ -f "$dist/index.html" ] || { echo "FAIL: $dist/index.html 不存在（先 bash scripts/sync-webui.sh）"; exit 1; }

violations=0

# 1) index.html 中以 http(s) 加载的资源
if grep -nE '<(link|script|img)[^>]+(src|href)="https?://' "$dist/index.html"; then
  echo "FAIL: index.html 存在外链资源"; violations=$((violations+1))
fi

# 2) CSS 中的外部 url() 与 @import
while IFS= read -r f; do
  if grep -noE 'url\((["'"'"']?)https?://|@import\s+(url\()?["'"'"']?https?://' "$f"; then
    echo "FAIL: $f 存在外链样式资源"; violations=$((violations+1))
  fi
done < <(find "$dist" -name '*.css' -type f)

# 3) 信息项：JS 内出现的 https 字符串（不判失败）
js_hits=$(grep -rhoE 'https?://[a-zA-Z0-9.-]+' "$dist"/js "$dist"/jse 2>/dev/null | sort -u | head -8 || true)
[ -n "$js_hits" ] && { echo "INFO(js 内字符串 URL，非加载引用):"; echo "$js_hits"; }

if [ "$violations" -gt 0 ]; then
  echo "RESULT: FAIL ($violations 项外链违规)"; exit 1
fi
echo "RESULT: PASS — 产物零外链资源，内网离线可用"
