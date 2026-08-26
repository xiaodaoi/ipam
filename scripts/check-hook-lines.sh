#!/usr/bin/env bash
# K4 硬门禁：hook C++ 有效行数（去空行与纯注释）必须 <800 行
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
total=0
while IFS= read -r f; do
  n=$(grep -cvE '^\s*(//.*|/\*|\*|$)' "$f" || true)
  total=$((total + n))
  printf '%5d  %s\n' "$n" "${f#"$root"/}"
done < <(find "$root/hook-coherence/src" -name '*.cc' -o -name '*.h' | sort)
echo "TOTAL=$total (budget <800)"
[ "$total" -lt 800 ] || { echo "FAIL: 超出 K4 行数预算"; exit 1; }
