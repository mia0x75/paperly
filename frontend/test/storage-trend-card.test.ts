/**
 * test/storage-trend-card.test.ts -- dual-scale contract of the storage growth card.
 *
 * The data assembly is pinned in storage-trend.test; this mounts the actual card and
 * inspects the Chart.js instance (via Chart.getChart on the canvas) for the config
 * that keeps the database line readable: the two series sit on two hidden autofit
 * axes, so the MB/day image files never flatten the ~1-2MB (per 12 years) database
 * curve against zero.
 */
import "./setup";
import { Chart } from "chart.js";
import { assert, assertEqual, assertIncludes, type TestSuite } from "./assert";
import { mountPage } from "./mount";
import StorageTrendCard from "../src/components/StorageTrendCard.vue";

const TREND = {
  days: 30,
  points: [
    { date: "2026-09-28", db_size_kb: 2048, assets_size_kb: 1048576 },
    { date: "2026-09-29", db_size_kb: 2560, assets_size_kb: 1054720 },
    { date: "2026-09-30", db_size_kb: 3072, assets_size_kb: 1060864 },
  ],
};

const suite: TestSuite = {
  name: "存储增长卡（数据库/文件资源分轴）",
  cases: [
    ["数据库与文件资源分用两轴，避免共享刻度压平数据库线", async () => {
      const r = await mountPage(StorageTrendCard, {
        responses: { "/api/metrics/storage-trend": TREND },
      });
      const canvas = r.doc.querySelector("canvas");
      assert(canvas !== null, "应渲染图表画布");
      const chart = Chart.getChart(canvas as unknown as HTMLCanvasElement);
      assert(chart !== undefined, "应能取到 Chart.js 实例");

      const yAxisIDs = chart!.data.datasets.map((ds) => (ds as { yAxisID?: string }).yAxisID as string);
      assertEqual(yAxisIDs[0], "y", "数据库（第 1 条线）使用主轴");
      assertEqual(yAxisIDs[1], "y1", "文件资源（第 2 条线）使用独立轴");
      assert(
        yAxisIDs[0] !== yAxisIDs[1],
        "两条线不能在同一个刻度尺上：否则小字节的数据库线被压成零线",
      );

      const scales = chart!.options.scales as Record<string, unknown>;
      assert(scales["y1"] !== undefined, "options 应声明 y1 轴");
      assertEqual((scales["y"] as { display?: boolean }).display, false, "y 轴隐藏（只表达趋势）");
      assertEqual((scales["y1"] as { display?: boolean }).display, false, "y1 轴同样隐藏");
      assertEqual(
        (scales["y1"] as { beginAtZero?: boolean }).beginAtZero,
        false,
        "y1 自动贴合文件资源自身范围",
      );
      assertEqual(r.errors.length, 0, `不应有未捕获异常：${r.errors.join(" | ")}`);
    }],

    ["空数据走空态，不挂分轴配置", async () => {
      const r = await mountPage(StorageTrendCard, {
        responses: { "/api/metrics/storage-trend": { days: 30, points: [] } },
      });
      assertIncludes(r.text, "暂无存储趋势", "空态标题");
      assertEqual(r.doc.querySelector("canvas"), null, "空态不渲染画布");
      assertEqual(r.errors.length, 0, `不应有未捕获异常：${r.errors.join(" | ")}`);
    }],

    ["Legend 用 DOM chips，与「上传坚持度」同款（移动 accent / 桌面 hover）", async () => {
      const r = await mountPage(StorageTrendCard, {
        responses: { "/api/metrics/storage-trend": TREND },
      });
      const legend = r.doc.querySelector('[data-testid="storage-legend"]');
      assert(legend !== null, "图表区域应有 DOM 图例（悬浮在画布顶部）");
      const raw = legend!.textContent ?? "";
      assertIncludes(raw, "数据库", "第 1 项");
      assertIncludes(raw, "文件资源", "第 2 项");
      assert(
        /\babsolute\b/.test(legend!.className) &&
          /\btop-0\b/.test(legend!.className) &&
          /\bleft-0\b/.test(legend!.className) &&
          !/\bjustify-center\b/.test(legend!.className),
        `图例应左对齐并悬浮覆盖在画布顶部预留带（不撑高卡片、不居中）：${legend!.className}`,
      );
      for (const chip of Array.from(legend!.querySelectorAll("span"))) {
        const cls = chip.className;
        if (cls.includes("inline-flex items-center gap-1")) {
          assert(
            /\bbg-accent\/60\b/.test(cls),
            `chip 应有移动端 accent 底：${cls}`,
          );
          assert(
            /\bsm:bg-transparent\b/.test(cls) && /\bhover:bg-accent\b/.test(cls),
            `chip 应有桌面端透明底 + hover：${cls}`,
          );
          assert(chip.querySelector("span") !== null, `chip 应有色点：${cls}`);
        }
      }
      assertEqual(r.errors.length, 0, `不应有未捕获异常：${r.errors.join(" | ")}`);
    }],
  ],
};

export default suite;