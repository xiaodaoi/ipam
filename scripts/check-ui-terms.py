#!/usr/bin/env python3
"""UI 对外术语门禁（M6-031）。

默认界面只允许出现业务词，不得出现底层产品名或实现细节，
否则客户可直接推断技术栈。允许保留的只有两类：

1. 法务归属：`views/_core/about/index.vue` 的开源组件清单（登记在本文件
   `ALLOWLIST` 指向的 `scripts/ui-terms-allowlist.txt`）；
2. 运维排障：确属诊断用途的行，在行尾加 `ipam-term-allow` 注释说明。

用法：
  python3 scripts/check-ui-terms.py             # 全量扫描，有命中则退出码 1
  python3 scripts/check-ui-terms.py --list      # 仅按文件汇总命中数
  python3 scripts/check-ui-terms.py --self-test # 自测（门禁本身是否有效）
"""
from __future__ import annotations

import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SRC = ROOT / "web/apps/web-ipam/src"
ALLOWLIST = ROOT / "scripts/ui-terms-allowlist.txt"
MARKER = "ipam-term-allow"
SKIP_NAMES = ("offline-icons", "schema.d.ts")
SUFFIXES = {".vue", ".ts", ".json"}

# 产品名用「非标识符边界」匹配：放过 keaSubnetId / pendingKea 这类内部字段名。
_TERMS = [
    r"(?<![A-Za-z0-9_$])PostgreSQL(?![A-Za-z0-9_$])",
    r"(?<![A-Za-z0-9_$])Postgres(?![A-Za-z0-9_$])",
    r"(?<![A-Za-z0-9_$])ClickHouse(?![A-Za-z0-9_$])",
    r"(?<![A-Za-z0-9_$])Kea(?![A-Za-z0-9_$])",
    r"(?<![A-Za-z0-9_$])Unbound(?![A-Za-z0-9_$])",
    r"(?<![A-Za-z0-9_$])Vector(?![A-Za-z0-9_$])",
    r"(?<![A-Za-z0-9_$])Parquet(?![A-Za-z0-9_$])",
    r"option-data",
    r"client-classes",
    r"lease_cmds",
    r"config-set",
    r"lease4-get-all",
    r"--watch-config",
    r"system\.parts",
]
TERM_RE = re.compile("|".join(_TERMS))


def mask_comments(src: str) -> str:
    """把注释替换为等长空格，保留换行与字符串内容，便于按行报错。"""
    out = list(src)
    i, n = 0, len(src)
    state: str | None = None
    while i < n:
        if state is None:
            if src.startswith("//", i):
                state = "line"
                out[i] = out[i + 1] = " "
                i += 2
                continue
            if src.startswith("/*", i):
                state = "block"
                out[i] = out[i + 1] = " "
                i += 2
                continue
            if src.startswith("<!--", i):
                state = "html"
                for k in range(4):
                    out[i + k] = " "
                i += 4
                continue
            if src[i] in "\"'`":
                quote = src[i]
                i += 1
                while i < n:
                    if src[i] == "\\":
                        i += 2
                        continue
                    if src[i] == quote:
                        i += 1
                        break
                    i += 1
                continue
            i += 1
        elif state == "line":
            if src[i] == "\n":
                state = None
            else:
                out[i] = " "
            i += 1
        elif state == "block":
            if src.startswith("*/", i):
                out[i] = out[i + 1] = " "
                state = None
                i += 2
                continue
            if src[i] != "\n":
                out[i] = " "
            i += 1
        else:  # html
            if src.startswith("-->", i):
                for k in range(3):
                    out[i + k] = " "
                state = None
                i += 3
                continue
            if src[i] != "\n":
                out[i] = " "
            i += 1
    return "".join(out)


def allow_prefixes() -> list[str]:
    if not ALLOWLIST.exists():
        return []
    return [
        line.strip()
        for line in ALLOWLIST.read_text(encoding="utf-8").splitlines()
        if line.strip() and not line.strip().startswith("#")
    ]


def scan(name: str, text: str, allow: list[str]) -> list[tuple[int, str, str]]:
    """返回 [(行号, 命中词, 原文行)]。"""
    if any(name.startswith(p) for p in allow):
        return []
    raw_lines = text.split("\n")
    masked_lines = mask_comments(text).split("\n")
    hits: list[tuple[int, str, str]] = []
    for lineno, (raw, masked) in enumerate(zip(raw_lines, masked_lines), 1):
        if MARKER in raw:
            continue
        for m in TERM_RE.finditer(masked):
            hits.append((lineno, m.group(0), raw.strip()))
    return hits


def collect() -> list[Path]:
    files: list[Path] = []
    for path in sorted(SRC.rglob("*")):
        if path.suffix not in SUFFIXES or not path.is_file():
            continue
        if any(part in str(path) for part in SKIP_NAMES):
            continue
        files.append(path)
    return files


def report(files: list[Path], allow: list[str], list_only: bool) -> int:
    total = 0
    by_file: dict[str, int] = {}
    lines: list[str] = []
    for path in files:
        rel = path.relative_to(ROOT).as_posix()
        hits = scan(rel, path.read_text(encoding="utf-8"), allow)
        if not hits:
            continue
        by_file[rel] = len(hits)
        total += len(hits)
        for lineno, term, text in hits:
            lines.append(f"{rel}:{lineno}: [{term}] {text}")

    if list_only:
        for rel, count in sorted(by_file.items(), key=lambda kv: -kv[1]):
            print(f"{count:4d}  {rel}")
        print(f"\n合计 {len(by_file)} 个文件 / {total} 处")
        return 0
    if lines:
        print("\n".join(lines))
        print(
            f"\n发现 {total} 处对外暴露的技术名/实现细节。"
            "请改为业务词，或确属诊断/法务用途时登记 allowlist 并加 ipam-term-allow。"
        )
        return 1
    print(f"OK: 界面文案无技术名残留（扫描 {len(files)} 个文件）")
    return 0


def self_test() -> int:
    cases = [
        ("clean.json", '{"desc": "配置数据库与日志存储"}', 0),
        ("dirty.json", '{"desc": "备份 PostgreSQL 与 ClickHouse"}', 2),
        ("ident.ts", "const pendingKea = 1; const keaRatio = 2;", 0),
        ("marker.ts", "postgres: 'PostgreSQL', // ipam-term-allow", 0),
        ("comment.ts", "// 说明：由 Kea 下发\nconst a = 1;", 0),
        ("detail.json", '{"meta": "lease_cmds · system.parts"}', 2),
    ]
    failures = []
    for name, text, expected in cases:
        got = len(scan(name, text, []))
        if got != expected:
            failures.append(f"{name}: expected {expected} hits, got {got}")
    allow = ["legal/page.vue"]
    if scan("legal/page.vue", "ClickHouse", allow):
        failures.append("allowlist prefix did not suppress hit")
    if failures:
        print("\n".join(failures), file=sys.stderr)
        return 1
    print(f"OK: 自测 {len(cases) + 1} 个用例通过")
    return 0


def main() -> int:
    if "--self-test" in sys.argv:
        return self_test()
    allow = allow_prefixes()
    return report(collect(), allow, "--list" in sys.argv)


if __name__ == "__main__":
    sys.exit(main())
