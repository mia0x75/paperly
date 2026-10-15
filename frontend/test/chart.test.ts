/**
 * test/chart.test.ts -- Chart.js theme factory (src/theme/chart.ts).
 *
 * Only locks "data shape" contracts, not pixels:
 *  - lines are pure curves (fill: false), no area fill;
 *  - each series takes a palette color, wrapping when the series count exceeds
 *    the palette length;
 *  - key option switches for axes/grid/tooltip (hidden vertical lines, dashed grid,
 *    index hit).
 *
 * No screenshot comparison: happy-dom has no layout engine, so Chart.js gets no real
 * canvas context and render output is unassertable; fill/color-picking are pure data
 * products and can be checked directly.
 */
import { assert, assertEqual, assertNotIncludes, type TestSuite } from "./assert";
import {
  DOT_SIZE,
  LINE_SERIES_COLOR_LIMIT,
  assignSubjectColors,
  buildChartTheme,
  buildLineData,
  buildLineOptions,
} from "../src/theme/chart";

const THEME = buildChartTheme("dark");

/**
 * Color-picking cases must inject a **known palette**, not buildChartTheme()'s output:
 * chartPalette() reads CSS variables, and happy-dom doesn't load tokens.css, so the
 * returned value is an empty string -- the assertion would degenerate to a "" === ""
 * no-op that is always true.
 */
const PALETTE = ["#aaaaaa", "#bbbbbb", "#cccccc"];
const THEME_WITH_PALETTE = { ...THEME, palette: PALETTE } as typeof THEME;

const SERIES = [
  { name: "小红1月的归档数", data: [1, 3] },
  { name: "小红2月的归档数", data: [2, 4] },
];

const suite: TestSuite = {
  name: "Chart.js 主题工厂（纯折线 / 取色 / 坐标轴）",
  cases: [
    ["工具口图例显式写 fontColor：暗色下不沿用画布残留色", () => {
      // Regression: when custom generateLabels omits fontColor, drawLegend falls
      // back to the previous frame's canvas fill color (persistently white-on-white
      // text in dark mode). A known theme is injected explicitly here to verify the write.
      const theme = { ...THEME, legendText: "#efefef", palette: ["#112233"] } as typeof THEME;
      const o = buildLineOptions(theme) as unknown as {
        plugins: {
          legend: {
            labels: {
              generateLabels: (chart: unknown) => Array<Record<string, unknown>>;
            };
          };
        };
      };
      const generated = o.plugins.legend.labels.generateLabels({
        data: { datasets: [{ label: "小明", borderColor: "#22cc88" }] },
        isDatasetVisible: () => true,
      });
      assertEqual(generated[0]?.fontColor, "#efefef", "图例文字色必须随主题显式写出");
      assertEqual(generated[0]?.fillStyle, "#22cc88", "圆点仍取曲线全色界");
      assertEqual(generated.length, 1, "每系列一项");
    }],

    ["图例与 hover 圆点对齐存储增长卡：直径 7px、行距适度放开", () => {
      const o = buildLineOptions(THEME) as unknown as {
        plugins: {
          legend: { labels: { usePointStyle: boolean; pointStyle: string; boxWidth: number; boxHeight: number } };
          tooltip: { usePointStyle: boolean; boxWidth: number; boxHeight: number; bodySpacing: number };
        };
      };
      const legend = o.plugins.legend.labels;
      const tip = o.plugins.tooltip;
      // Unified spec: tooltip radius = min(boxWidth, boxHeight)/2, legend radius
      // = boxHeight*√2/2. Both must finally compute a DOT_SIZE/2 radius (StorageTrendCard
      // legend `r = DOT_SIZE/2`), else dot sizes are inconsistent within the same dashboard.
      assertEqual(tip.usePointStyle, true, "tooltip 要画圆点而非色块");
      assertEqual(tip.boxWidth, DOT_SIZE, `tooltip 圆点 ${DOT_SIZE}px 直径`);
      assertEqual(tip.boxHeight, DOT_SIZE, `tooltip 圆点 ${DOT_SIZE}px 直径`);
      assertEqual(tip.bodySpacing, 6, "tooltip 行距应稍放开（默认 2 太挤）");
      assertEqual(legend.usePointStyle, true, "图例要画圆点");
      assertEqual(legend.pointStyle, "circle", "图例圆点形状");
      assertEqual(legend.boxWidth, DOT_SIZE, `图例项布局宽 ${DOT_SIZE}px`);
      assertEqual(
        Math.abs(legend.boxHeight - DOT_SIZE / Math.SQRT2) < 1e-9,
        true,
        `图例 boxHeight 应为 ${DOT_SIZE}/√2（半径 ${DOT_SIZE / 2}px）`,
      );
      // The converted radius must exactly equal the storage card's DOT_SIZE/2
      assertEqual(
        Math.abs((legend.boxHeight * Math.SQRT2) / 2 - DOT_SIZE / 2) < 1e-9,
        true,
        `图例圆点半径应为 ${DOT_SIZE / 2}px`,
      );
    }],

    ["折线不填充：fill 为 false 且无背景填充色", () => {
      const d = buildLineData(THEME, ["1日", "2日", "3日", "4日"], SERIES);
      for (const ds of d.datasets) {
        const rec = ds as Record<string, unknown>;
        assertEqual(rec.fill, false, "折线必须是不填充的纯曲线");
        assert(!("backgroundColor" in rec), "纯曲线不应再带区域填充色");
      }
    }],

    ["类别标签与数据原样传给 Chart.js", () => {
      const d = buildLineData(THEME_WITH_PALETTE, ["1日", "2日"], SERIES.slice(0, 1));
      assertEqual(d.labels.length, 2, "labels 数量");
      assertEqual(d.labels[0], "1日", "labels 原样透传");
      assertEqual((d.datasets[0] as { data: number[] }).data.join(","), "1,3", "数据原样透传");
    }],

    ["每个系列按调色板顺序取色", () => {
      const ds = datasets(buildLineData(THEME_WITH_PALETTE, ["a"], SERIES));
      assertEqual(ds[0]?.borderColor, PALETTE[0], "第 1 个系列取调色板首色");
      assertEqual(ds[1]?.borderColor, PALETTE[1], "第 2 个系列取调色板第二色");
    }],

    ["系列数超过设计调色板长度时用补色扩展而非重复（12 色上限）", () => {
      // The persistence overlay draws one line per kid: a 6th kid must not reuse the
      // 1st kid's color. Beyond the design palette (3 here), golden-angle oklch
      // supplements extend it to the 12-color ceiling; only past 12 does it wrap.
      const many = Array.from({ length: LINE_SERIES_COLOR_LIMIT + 2 }, (_, i) => ({
        name: `s${i}`,
        data: [i],
      }));
      const ds = datasets(buildLineData(THEME_WITH_PALETTE, ["a"], many));
      assertEqual(ds.length, LINE_SERIES_COLOR_LIMIT + 2, "系列数量");
      assertEqual(ds[0]?.borderColor, PALETTE[0], "第 1 个系列取调色板首色");
      assert(
        String(ds[PALETTE.length]?.borderColor).startsWith("oklch("),
        `第 N+1 个系列应是 oklch 补色，实际 ${String(ds[PALETTE.length]?.borderColor)}`,
      );
      const withinCeiling = ds.slice(0, LINE_SERIES_COLOR_LIMIT).map((d) => String(d.borderColor));
      assertEqual(new Set(withinCeiling).size, LINE_SERIES_COLOR_LIMIT, "12 色上限内两两不同");
      assertEqual(ds[LINE_SERIES_COLOR_LIMIT]?.borderColor, PALETTE[0], "第 13 个系列回到首色");
      assert(
        ds.every((d) => typeof d.borderColor === "string" && d.borderColor !== ""),
        "任何系列都必须拿到颜色",
      );
    }],

    ["科目配色：每个科目互不相同（超过调色板也不撞色）", () => {
      // The exam has ~10 subjects, far beyond the palette capacity (3): an
      // implementation using subject_id % 3 would give subjects 1/4/7, 2/5/8,
      // 3/6/9 the same color, making sectors indistinguishable.
      const ids = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10];
      const map = assignSubjectColors(ids, PALETTE);
      const colors = Array.from(map.values());
      assertEqual(colors.length, ids.length, "每科都有颜色");
      assertEqual(new Set(colors).size, ids.length, "颜色两两不同");
      // The top few use the palette (same swatches as site-wide charts)
      assertEqual(map.get(1), PALETTE[0], "第 1 科取调色板首色");
      assertEqual(map.get(2), PALETTE[1], "第 2 科取调色板第二色");
      assertEqual(map.get(3), PALETTE[2], "第 3 科取调色板第三色");
      // Beyond it, complementary colors from the golden angle (oklch), still valid color strings
      const extra = map.get(4);
      assert(
        extra !== undefined && extra.startsWith("oklch("),
        `补充色应走 oklch，实际 ${extra}`,
      );
    }],

    ["科目配色：按 subject_id 名次分配，与传入顺序无关", () => {
      // Data ordering by count changes, but the rank (= subject_id order) doesn't,
      // so a subject's color stays put -- whether passed ascending or descending.
      const asc = assignSubjectColors([1, 2, 3, 4], PALETTE);
      const desc = assignSubjectColors([4, 3, 2, 1], PALETTE);
      assertEqual(asc.get(1), desc.get(1), "科目 1 颜色不随传入顺序跳");
      assertEqual(asc.get(4), desc.get(4), "科目 4 颜色不随传入顺序跳");
    }],

    ["坐标轴：隐藏竖网格线、网格为虚线", () => {
      const o = buildLineOptions(THEME) as unknown as {
        scales: Record<string, { grid: { display: boolean; borderDash: number[] } }>;
      };
      const x = o.scales.x;
      const y = o.scales.y;
      if (!x || !y) throw new Error("应同时产出 x / y 两轴配置");
      assertEqual(x.grid.display, false, "竖网格线应隐藏");
      assert(Array.isArray(y.grid.borderDash), "横网格线应为虚线");
    }],

    ["tooltip 按 index 命中整行，point 只在 hover 放大", () => {
      const o = buildLineOptions(THEME) as unknown as { interaction: { mode: string; intersect: boolean } };
      assertEqual(o.interaction.mode, "index", "tooltip 应按 index 命中");
      assertEqual(o.interaction.intersect, false, "不需要精确命中数据点");
      const d = buildLineData(THEME, ["a"], SERIES.slice(0, 1));
      const rec = d.datasets[0] as Record<string, unknown>;
      assertEqual(rec.pointRadius, 0, "常态不画点");
      assertEqual(rec.pointHoverRadius, 4, "hover 时放大");
    }],

    ["主题片段不含已废弃的填充透明度", () => {
      const t = THEME as unknown as Record<string, unknown>;
      assert(!("fillOpacity" in t), "fillOpacity 随纯曲线改造一并移除");
      assertNotIncludes(JSON.stringify(buildLineOptions(THEME)), "areaStyle", "不应残留 ECharts 的 areaStyle");
    }],
  ],
};

/** Narrow datasets to a borderColor-bearing shape, avoiding casts all over the assertions. */
function datasets(d: { datasets: Record<string, unknown>[] }): Array<{ borderColor?: unknown }> {
  return d.datasets as Array<{ borderColor?: unknown }>;
}

export default suite;
