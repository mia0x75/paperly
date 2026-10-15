/**
 * test/tokens.test.ts -- design token readability and layering constraints.
 *
 * Static parsing rather than getComputedStyle: happy-dom doesn't load the built
 * CSS, so getComputedStyle(document.documentElement).getPropertyValue('--background')
 * returns empty. Here we read the `/* #hex *\/` comments directly from the source
 * to get concrete colors, then compute contrast.
 *
 * Four properties are locked (break any and the test goes red):
 *   1. light canvas ≠ card -- background/foreground layering, else a full screen of
 *      pure white over near-black text is glaring;
 *   2. body text is not near-pure-black and has ≥ 7:1 (AAA) contrast on canvas/card;
 *   3. secondary text (`text-muted-foreground`: timestamps, page numbers, status
 *      badges) ≥ 4.5:1 (AA) on both canvas and card -- it's `text-xs`;
 *   4. destructive color ≥ 4.5:1 (AA) on both canvas and card.
 *
 * Dark mode only checks layering, not contrast: body text is light on dark, the
 * same relation as light mode.
 */
import { assert, assertEqual, type TestSuite } from "./assert";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const TOKENS_PATH = fileURLToPath(new URL("../src/theme/tokens.css", import.meta.url));

/** Extract the `--name` -> `#hex` mapping inside the `:root { ... }` (light) block. */
function lightTokens(): Record<string, string> {
  const css = readFileSync(TOKENS_PATH, "utf8");
  const start = css.indexOf(":root {");
  assert(start >= 0, "tokens.css 应有 :root 亮色块");
  const end = css.indexOf("\n}", start);
  const block = css.slice(start, end);
  const out: Record<string, string> = {};
  // `--name: <any value>; /* #rrggbb */` -- colors are annotated via hex comments,
  // so real values can be read here
  for (const m of block.matchAll(/--([a-z0-9-]+)\s*:[^;]*;\s*\/\*\s*(#[0-9a-fA-F]{6})\s*\*\//g)) {
    const name = m[1];
    const hex = m[2];
    if (name === undefined || hex === undefined) continue;
    out[name] = hex;
  }
  return out;
}

function channels(hex: string): [number, number, number] {
  const h = hex.replace("#", "");
  return [
    parseInt(h.slice(0, 2), 16),
    parseInt(h.slice(2, 4), 16),
    parseInt(h.slice(4, 6), 16),
  ];
}

/** sRGB -> linear luminance (the component curve of the WCAG 2.x relative-luminance formula). */
function channel(c: number): number {
  const v = c / 255;
  return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
}

function luminance(hex: string): number {
  const [r, g, b] = channels(hex);
  return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
}

/** WCAG contrast, returned rounded to two decimal places for readable error messages. */
function contrast(a: string, b: string): number {
  const la = luminance(a);
  const lb = luminance(b);
  const ratio = (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
  return Math.round(ratio * 100) / 100;
}

const T = lightTokens();

/** Extract the token mapping of the `:root.dark { ... }` (dark) block. */
function darkTokens(): Record<string, string> {
  const css = readFileSync(TOKENS_PATH, "utf8");
  const start = css.indexOf(":root.dark {");
  assert(start >= 0, "tokens.css 应有 :root.dark 暗色块");
  const end = css.indexOf("\n}", start);
  const block = css.slice(start, end);
  const out: Record<string, string> = {};
  for (const m of block.matchAll(/--([a-z0-9-]+)\s*:[^;]*;\s*\/\*\s*(#[0-9a-fA-F]{6})\s*\*\//g)) {
    const name = m[1];
    const hex = m[2];
    if (name === undefined || hex === undefined) continue;
    out[name] = hex;
  }
  return out;
}

const D = darkTokens();

function needDark(name: string): string {
  const v = D[name];
  if (v === undefined) throw new Error(`tokens.css 暗色块缺少带 hex 注释的 --${name}`);
  return v;
}

/** Required tokens; a missing one means tokens.css's comment format changed. */
function need(name: string): string {
  const v = T[name];
  if (v === undefined) throw new Error(`tokens.css 亮色块缺少带 hex 注释的 --${name}`);
  return v;
}

const suite: TestSuite = {
  name: "设计 token（亮色：分层 + WCAG 对比度）",
  cases: [
    ["token 文件里每个关键色都带 hex 注释（测试的取值前提）", () => {
      for (const name of ["background", "foreground", "card", "muted-foreground", "control", "border", "destructive", "destructive-foreground", "success", "success-foreground", "warning", "warning-foreground"]) {
        need(name);
      }
      assert(true, "关键 token 均可解析");
    }],

    ["亮色画布与卡片不同色（有底/面分层）", () => {
      const bg = need("background");
      const card = need("card");
      assert(bg !== card, `画布(${bg}) 与卡片(${card}) 不能同色，否则卡片浮不起来`);
      // The card must be lighter than the canvas (in light mode it tends toward white)
      assert(
        luminance(card) > luminance(bg),
        `亮色主题下卡片应比画布亮，实际 card=${card} bg=${bg}`,
      );
    }],

    ["正文不是近纯黑（避免刺眼）", () => {
      const fg = need("foreground");
      assert(fg !== "#000000", "禁止纯黑 #000000（AGENTS.md UI 守则）");
      assert(fg !== "#0a0a0a", "旧值 #0a0a0a 接近纯黑");
      // Soft range: luminance no lower than the zinc-800 level (#27272a ≈ 0.0205)
      assert(luminance(fg) >= 0.012, `正文 ${fg} 过暗，会比旧值更刺眼`);
    }],

    ["正文对画布与卡片均 ≥ 7:1（AAA）", () => {
      const fg = need("foreground");
      for (const bgName of ["background", "card"]) {
        const c = contrast(fg, need(bgName));
        assert(c >= 7, `正文对 ${bgName} 仅 ${c}:1，需 ≥ 7:1`);
      }
    }],

    ["次要文字对画布与卡片均 ≥ 4.5:1（AA，且它是 text-xs）", () => {
      const muted = need("muted-foreground");
      for (const bgName of ["background", "card"]) {
        const c = contrast(muted, need(bgName));
        assert(c >= 4.5, `次要文字对 ${bgName} 仅 ${c}:1，需 ≥ 4.5:1`);
      }
    }],

    ["次要文字在控件底（control）上也 ≥ 4.5:1", () => {
      // Form placeholders / helper text sit on the control fill, not the card
      const c = contrast(need("muted-foreground"), need("control"));
      assert(c >= 4.5, `次要文字对控件底仅 ${c}:1`);
    }],

    ["危险色对画布与卡片均 ≥ 4.5:1（AA）", () => {
      const c1 = contrast(need("destructive"), need("background"));
      const c2 = contrast(need("destructive"), need("card"));
      assert(c1 >= 4.5, `危险色对画布仅 ${c1}:1`);
      assert(c2 >= 4.5, `危险色对卡片仅 ${c2}:1`);
    }],

    ["边框与输入框分两级，控件边界看得出来", () => {
      // border and input must differ; the same #e5e5e5 smears control boundaries
      const border = need("border");
      const input = need("input");
      assert(border !== input, `border(${border}) 与 input(${input}) 不应同色`);
      // input must be one shade darker than border
      assert(luminance(input) < luminance(border), `input 应比 border 更深，实际 ${input} vs ${border}`);
    }],

    ["控件底与卡片有可见差异（不是同一个白）", () => {
      const c = contrast(need("control"), need("card"));
      assert(c > 1.03, `控件底与卡片几乎同色（${c}:1），输入框会看不出边界`);
    }],

    ["主按钮文字对主按钮底 ≥ 7:1", () => {
      const c = contrast(need("primary-foreground"), need("primary"));
      assert(c >= 7, `主按钮仅 ${c}:1`);
    }],

    ["语义填充文字对填充底均 ≥ 4.5:1（AA，徽章/危险按钮小字）", () => {
      // White text fails on the teal/amber fills and on dark destructive, so each
      // semantic fill has its own foreground token; lock all six pairs here.
      const pairs: Array<[string, string]> = [
        ["destructive-foreground", "destructive"],
        ["success-foreground", "success"],
        ["warning-foreground", "warning"],
      ];
      for (const [fg, bg] of pairs) {
        const c = contrast(need(fg), need(bg));
        assert(c >= 4.5, `亮色 ${fg} 对 ${bg} 仅 ${c}:1，需 ≥ 4.5:1`);
      }
      for (const [fg, bg] of pairs) {
        const c = contrast(needDark(fg), needDark(bg));
        assert(c >= 4.5, `暗色 ${fg} 对 ${bg} 仅 ${c}:1，需 ≥ 4.5:1`);
      }
    }],

    ["亮色 primary 与正文同色系（不再是纯黑 #171717）", () => {
      // primary must match foreground; #171717 would be darker than body text and stand out
      assertEqual(need("primary"), need("foreground"), "primary 应与 foreground 同值（暗色下按钮才不突兀）");
    }],

    ["亮色按钮底色加重：控件底明显深于卡片，边框更深一层", () => {
      // Light-mode control must be noticeably darker than card (#f4f4f5 is only
      // 1.03:1 against a white card), and the border heavier than the control fill
      // so the button outline stands.
      const control = need("control");
      assert(contrast(control, need("card")) >= 1.15, `控件底 ${control} 对卡片不够重`);
      assert(
        luminance(need("border")) < luminance(control),
        `边框 ${need("border")} 应比控件底 ${control} 更深`,
      );
    }],

    ["亮色背景圆点可见但不抢戏（旧 #e4e4e4 太淡）", () => {
      const dot = need("pattern-dot");
      const bg = need("background");
      assert(contrast(dot, bg) >= 1.4, `亮色圆点 ${dot} 对画布 ${bg} 对比 ${contrast(dot, bg)}:1，太淡看不见`);
      assert(contrast(dot, bg) <= 3, `亮色圆点 ${dot} 过深，会抢卡片内容`);
    }],

    ["暗色背景圆点可见但不刺眼（上一版几乎看不见）", () => {
      const dot = needDark("pattern-dot");
      const bg = needDark("background");
      // Roughly 2:1 brighter than the canvas: the 1px dot texture is visible without
      // being as glaring as the light-mode dot.
      assert(luminance(dot) > luminance(bg), "暗色圆点应比画布亮，否则看不见");
      assert(contrast(dot, bg) >= 1.8, `暗色圆点 ${dot} 对画布 ${bg} 仅 ${contrast(dot, bg)}:1，仍近乎隐形`);
      assert(luminance(dot) < 0.12, `暗色圆点 ${dot} 过亮，暗色下会发刺`);
    }],

    ["暗色 primary 是深灰实底，不是近白", () => {
      // #e5e5e5 is near-white -> the default variant is a white fill in dark mode,
      // clashing with the outline's dark fill
      const p = needDark("primary");
      assert(luminance(p) < 0.2, `暗色 primary ${p} 仍偏亮，应为深灰实底`);
      assert(contrast(needDark("primary-foreground"), p) >= 7, "暗色 primary-foreground 对 primary 需 ≥ 7:1");
    }],
  ],
};

export default suite;
