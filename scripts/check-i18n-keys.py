#!/usr/bin/env python3
"""locale key 结构一致性检查（M6-025）。

对比 `locales/langs/zh-CN` 与 `en-US` 下同名 json 文件的嵌套 key 结构，
以及占位符集合（`{name}`）是否一致。缺失/多余/占位符不同均失败。

用法：
  python3 scripts/check-i18n-keys.py
"""
from __future__ import annotations

import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
LANGS = ROOT / "web/apps/web-ipam/src/locales/langs"
BASE, TARGET = "zh-CN", "en-US"
PLACEHOLDER = re.compile(r"\{(\w+)\}")


def flatten(obj: object, prefix: str = "") -> dict[str, object]:
    out: dict[str, object] = {}
    if isinstance(obj, dict):
        for key, value in obj.items():
            path = f"{prefix}.{key}" if prefix else key
            out.update(flatten(value, path))
    else:
        out[prefix] = obj
    return out


def main() -> int:
    base_dir, target_dir = LANGS / BASE, LANGS / TARGET
    if not base_dir.is_dir() or not target_dir.is_dir():
        print(f"缺少语言目录: {base_dir} / {target_dir}")
        return 1

    problems: list[str] = []
    base_files = sorted(p.name for p in base_dir.glob("*.json"))
    target_files = sorted(p.name for p in target_dir.glob("*.json"))
    for name in sorted(set(base_files) ^ set(target_files)):
        problems.append(f"文件仅存在于一侧: {name}")

    for name in sorted(set(base_files) & set(target_files)):
        base = flatten(json.loads((base_dir / name).read_text(encoding="utf-8")))
        target = flatten(json.loads((target_dir / name).read_text(encoding="utf-8")))
        for key in sorted(set(base) - set(target)):
            problems.append(f"{name}: {BASE} 有而 {TARGET} 缺失 -> {key}")
        for key in sorted(set(target) - set(base)):
            problems.append(f"{name}: {TARGET} 多余 -> {key}")
        for key in sorted(set(base) & set(target)):
            b, t = base[key], target[key]
            if not isinstance(b, str) or not isinstance(t, str):
                continue
            pb, pt = set(PLACEHOLDER.findall(b)), set(PLACEHOLDER.findall(t))
            if pb != pt:
                problems.append(
                    f"{name}: 占位符不一致 {key}: {BASE}={sorted(pb)} {TARGET}={sorted(pt)}"
                )

    if problems:
        print("\n".join(problems))
        print(f"\n共 {len(problems)} 处不一致")
        return 1
    print(f"OK: locale key 结构一致（{len(base_files)} 个文件）")
    return 0


if __name__ == "__main__":
    sys.exit(main())
