#!/usr/bin/env python3
"""重生成 Material Symbols Rounded 图标子集（Paperly 自托管 woff2）。

码位唯一来源：frontend/src/components/Icon.vue 的 CODEPOINT（PUA 寻址），
本脚本自动解析、去重、排序，不手写码位表，避免与 Icon.vue 脱节。
（cancel/clear 同码 0xe5c9，去重后 57 条目 → 56 unicodes → 57 glyphs 含 .notdef）

依赖：fonttools（含 brotli，以输出 woff2），即 pip install "fonttools[brotli]"。

用法：
    python3 frontend/scripts/subset_icons.py --check        # 只解析打印，不写文件
    python3 frontend/scripts/subset_icons.py                # 按默认路径重生成
    python3 frontend/scripts/subset_icons.py -o /tmp/x.woff2  # 输出到别处比对
    python3 frontend/scripts/subset_icons.py -s /path/to/full.ttf

默认源字体（Regular 400，与线上子集 add 字形一致）：
    /home/mxu/.local/share/fonts/MaterialSymbols/material-symbols-rounded-5.ttf
默认输出：
    frontend/src/assets/fonts/material-symbols-rounded.woff2
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from pathlib import Path

SCRIPT_DIR = Path(__file__).resolve().parent
REPO_ROOT = SCRIPT_DIR.parents[1]  # frontend/scripts/ → paperly/
DEFAULT_ICON_VUE = REPO_ROOT / "frontend/src/components/Icon.vue"
DEFAULT_SOURCE = Path(
    "/home/mxu/.local/share/fonts/MaterialSymbols/material-symbols-rounded-5.ttf"
)
DEFAULT_OUTPUT = REPO_ROOT / "frontend/src/assets/fonts/material-symbols-rounded.woff2"

# PUA 私用区：Material Symbols 码位全部落在此区间，借此过滤掉 Icon.vue 里
# fill/weight 等非码位数字（如 400），只取真正的字形码位。
PUA_MIN, PUA_MAX = 0xE000, 0xF8FF
CODEPOINT_RE = re.compile(r"0x([0-9a-fA-F]{4,6})")


def extract_unicodes(icon_vue: Path) -> list[int]:
    """从 Icon.vue 解析 CODEPOINT，去重排序。"""
    text = icon_vue.read_text(encoding="utf-8")
    codepoints = {
        int(m.group(1), 16)
        for m in CODEPOINT_RE.finditer(text)
        if PUA_MIN <= int(m.group(1), 16) <= PUA_MAX
    }
    if not codepoints:
        raise SystemExit(f"未在 {icon_vue} 解析到任何 PUA 码位，放弃")
    return sorted(codepoints)


def unicodes_arg(codepoints: list[int]) -> str:
    return ",".join(f"U+{cp:04X}" for cp in codepoints)


def run_subset(source: Path, output: Path, unicodes: str) -> None:
    if not source.is_file():
        raise SystemExit(f"源字体不存在：{source}（可用 -s 指定）")
    cmd = [
        sys.executable,
        "-m",
        "fontTools.subset",
        str(source),
        f"--output-file={output}",
        "--flavor=woff2",
        f"--unicodes={unicodes}",
    ]
    print("+ " + " ".join(cmd))
    subprocess.run(cmd, check=True)


def verify(output: Path, expected_glyphs: int) -> None:
    """校验：glyph 数 = 码位数 + 1（.notdef），upem 保持 960。"""
    from fontTools.ttLib import TTFont

    font = TTFont(output)
    n = font["maxp"].numGlyphs
    upem = font["head"].unitsPerEm
    size = output.stat().st_size
    print(f"产物：{size} bytes，glyphs={n}（期望 {expected_glyphs}），upem={upem}")
    if n != expected_glyphs:
        raise SystemExit(f"glyph 数不符：{n} != {expected_glyphs}")
    if upem != 960:
        raise SystemExit(f"upem 不符：{upem} != 960")


def main() -> None:
    ap = argparse.ArgumentParser(description="重生成图标子集 woff2")
    ap.add_argument("-s", "--source", type=Path, default=DEFAULT_SOURCE)
    ap.add_argument("-o", "--output", type=Path, default=DEFAULT_OUTPUT)
    ap.add_argument("--icon-vue", type=Path, default=DEFAULT_ICON_VUE)
    ap.add_argument(
        "--check", action="store_true", help="只解析并打印码位，不写文件"
    )
    ap.add_argument("--no-verify", action="store_true", help="跳过产物校验")
    args = ap.parse_args()

    codepoints = extract_unicodes(args.icon_vue)
    unicodes = unicodes_arg(codepoints)
    print(f"码位 {len(codepoints)} 个（去重后）：{unicodes}")
    if args.check:
        return

    run_subset(args.source, args.output, unicodes)
    if not args.no_verify:
        verify(args.output, len(codepoints) + 1)


if __name__ == "__main__":
    main()
