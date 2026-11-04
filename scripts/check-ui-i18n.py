#!/usr/bin/env python3
"""UI 文案 i18n 扫描（M6-025）。

用途：找出 `web/apps/web-ipam/src/views|components` 中**注释之外**仍含中日韩字符的行。
注释里的中文是允许的（代码说明保留中文）；正则/协议字段等确属非文案的，需在
`scripts/i18n-allowlist.txt` 中登记文件与行内容片段。

用法：
  python3 scripts/check-ui-i18n.py            # 全量扫描，有命中则退出码 1
  python3 scripts/check-ui-i18n.py --list     # 仅列出文件计数
"""
from __future__ import annotations

import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
UI_DIRS = [ROOT / "web/apps/web-ipam/src/views", ROOT / "web/apps/web-ipam/src/components"]
EXTRA_FILES = [
    ROOT / "web/apps/web-ipam/src/api/request.ts",
    ROOT / "web/apps/web-ipam/src/router/guard.ts",
    ROOT / "web/apps/web-ipam/src/layouts/basic.vue",
    ROOT / "web/apps/web-ipam/src/layouts/auth.vue",
]
ALLOWLIST = ROOT / "scripts/i18n-allowlist.txt"
CJK = re.compile(r"[\u3040-\u30ff\u3400-\u4dbf\u4e00-\u9fff]")


def strip_comments(src: str) -> str:
    """移除 //、/* */、<!-- --> 注释，保留字符串内容。"""
    out: list[str] = []
    i, n = 0, len(src)
    state: str | None = None
    while i < n:
        if state is None:
            if src.startswith("//", i):
                state, i = "line", i + 2
                continue
            if src.startswith("/*", i):
                state, i = "block", i + 2
                continue
            if src.startswith("<!--", i):
                state, i = "html", i + 4
                continue
            ch = src[i]
            if ch in "\"'`":
                quote = ch
                out.append(ch)
                i += 1
                while i < n:
                    if src[i] == "\\":
                        out.append(src[i : i + 2])
                        i += 2
                        continue
                    out.append(src[i])
                    if src[i] == quote:
                        i += 1
                        break
                    i += 1
                continue
            out.append(ch)
            i += 1
        elif state == "line":
            if src[i] == "\n":
                state = None
                out.append("\n")
            i += 1
        elif state == "block":
            if src.startswith("*/", i):
                state, i = None, i + 2
                continue
            i += 1
        else:  # html
            if src.startswith("-->", i):
                state, i = None, i + 3
                continue
            i += 1
    return "".join(out)


def allow_patterns() -> list[str]:
    if not ALLOWLIST.exists():
        return []
    return [
        line.strip()
        for line in ALLOWLIST.read_text(encoding="utf-8").splitlines()
        if line.strip() and not line.strip().startswith("#")
    ]


def main() -> int:
    list_only = "--list" in sys.argv
    files: list[Path] = []
    for d in UI_DIRS:
        files += [p for p in d.rglob("*") if p.suffix in {".vue", ".ts"}]
    files += [p for p in EXTRA_FILES if p.exists()]

    patterns = allow_patterns()
    total = 0
    by_file: list[tuple[int, Path]] = []
    hits: list[str] = []
    for path in sorted(files):
        if "offline-icons" in path.name or "schema.d.ts" in path.name:
            continue
        if path.name.endswith((".test.ts", ".test.vue")):
            continue  # 测试描述不算 UI 文案
        src = path.read_text(encoding="utf-8")
        for lineno, line in enumerate(strip_comments(src).splitlines(), 1):
            if not CJK.search(line):
                continue
            rel = path.relative_to(ROOT).as_posix()
            if any(p in rel or p in line for p in patterns):
                continue
            by_file.append((1, path))
            hits.append(f"{rel}:{lineno}: {line.strip()}")
            total += 1

    if list_only:
        counts: dict[str, int] = {}
        for _, p in by_file:
            rel = p.relative_to(ROOT).as_posix()
            counts[rel] = counts.get(rel, 0) + 1
        for rel, c in sorted(counts.items(), key=lambda kv: -kv[1]):
            print(f"{c:4d}  {rel}")
        print(f"\n合计 {len(counts)} 个文件 / {total} 行")
        return 0

    if hits:
        print("\n".join(hits))
        print(f"\n发现 {total} 行非注释中文，请抽取到 locale 文件或用 allowlist 说明。")
        return 1
    print(f"OK: UI 文案无硬编码中文（扫描 {len(files)} 个文件）")
    return 0


if __name__ == "__main__":
    sys.exit(main())
