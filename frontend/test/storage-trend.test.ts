/**
 * test/storage-trend.test.ts -- data assembly for the storage growth trend line
 * chart (src/lib/storageTrend).
 *
 * Pure logic, no DOM: locks in the three business rules of the 4th card --
 *   1. always two series (database / file assets), labels use MM-DD;
 *   2. missing samples (null) on/after the first sample pass through unchanged,
 *      never padded to 0 (padding 0 would forge a false valley in the chart) —
 *      only pre-history days (before the first sample ever) are zero-padded by
 *      padLeadingDays so a young system still draws a full-width axis;
 *   3. KB -> MB with two decimals.
 * The canvas legend is canvas-drawn, so the two-series guarantee is pinned on this
 * data-assembly contract instead of the DOM.
 */
import { assert, assertEqual, type TestSuite } from "./assert";
import { padLeadingDays, toLineSeries } from "../src/lib/storageTrend";
import type { StorageTrendPoint } from "../src/api/types";

const suite: TestSuite = {
  name: "存储增长趋势（双线折线数据组装）",
  cases: [
    ["恒为两条线，labels 取 MM-DD", async () => {
      const points: StorageTrendPoint[] = [
        { date: "2026-09-08", db_size_kb: 8192, assets_size_kb: 1048576 },
        { date: "2026-10-08", db_size_kb: 9216, assets_size_kb: 1049600 },
      ];
      const { labels, series } = toLineSeries(points);
      assertEqual(labels.length, 2, "labels 与点数等长");
      assertEqual(labels[0], "09-08", "labels 取月日");
      assertEqual(labels[1], "10-08", "labels 取月日");
      assertEqual(series.length, 2, "恒为两条线");
      assertEqual(series[0]?.name, "数据库", "第 1 条线（图例不带单位）");
      assertEqual(series[1]?.name, "文件资源", "第 2 条线（图例不带单位）");
    }],

    ["缺采 null 原样透传，绝不补 0", async () => {
      const points: StorageTrendPoint[] = [
        { date: "2026-10-07", db_size_kb: 8192, assets_size_kb: null },
        { date: "2026-10-08", db_size_kb: null, assets_size_kb: 1048576 },
      ];
      const { series } = toLineSeries(points);
      const db = series[0]?.data ?? [];
      const assets = series[1]?.data ?? [];
      assertEqual(db[1], null, "数据库缺采日必须是 null（断线）");
      assertEqual(assets[0], null, "文件资源缺采日必须是 null（断线）");
      assert(
        db[1] !== 0 && assets[0] !== 0,
        "缺采不得被当成 0——0 会让趋势图砸出假谷底",
      );
    }],

    ["KB → MB 两位小数", async () => {
      const points: StorageTrendPoint[] = [
        { date: "2026-10-08", db_size_kb: 8192, assets_size_kb: 1048576 },
      ];
      const { series } = toLineSeries(points);
      assertEqual(series[0]?.data[0], 8, "8192 KB = 8 MB");
      assertEqual(series[1]?.data[0], 1024, "1048576 KB = 1024 MB");
    }],

    ["半 MB 值保留两位小数", async () => {
      const points: StorageTrendPoint[] = [
        { date: "2026-10-08", db_size_kb: 512, assets_size_kb: 10 },
      ];
      const { series } = toLineSeries(points);
      assertEqual(series[0]?.data[0], 0.5, "512 KB = 0.5 MB");
      assertEqual(series[1]?.data[0], 0.01, "10 KB ≈ 0.01 MB");
    }],

    ["空序列不抛错（组件层先走空态，这里是防御）", async () => {
      const { labels, series } = toLineSeries([]);
      assertEqual(labels.length, 0, "空 labels");
      assertEqual(series.length, 2, "线的结构仍在（组件以 points 长度判空态）");
      assertEqual(series[0]?.data.length, 0, "空 data");
    }],

    ["新系统前导天补 0（缺几天补几天）", async () => {
      const points: StorageTrendPoint[] = [
        { date: "2026-10-06", db_size_kb: 8192, assets_size_kb: 1024 },
        { date: "2026-10-07", db_size_kb: 9216, assets_size_kb: 2048 },
      ];
      const padded = padLeadingDays(points, 5, "2026-10-07");
      assertEqual(padded.length, 5, "补足 5 天窗口");
      assertEqual(padded[0]?.date, "2026-10-03", "从窗口起点开始");
      assertEqual(padded[2]?.date, "2026-10-05", "前导最后一天");
      assertEqual(padded[0]?.db_size_kb, 0, "前导补 0");
      assertEqual(padded[2]?.assets_size_kb, 0, "前导补 0");
      assertEqual(padded[3]?.db_size_kb, 8192, "真实样本不动");
      // 补齐后走 toLineSeries 应画满 5 个标签、零值不断线
      const { labels, series } = toLineSeries(padded);
      assertEqual(labels.length, 5, "labels 满窗口");
      assertEqual(labels[0], "10-03", "首个标签是补齐日起点");
      assertEqual(series[0]?.data[0], 0, "零值进图表（不断线）");
      assertEqual(series[0]?.data[3], 8, "样本值正常换算");
    }],

    ["中间缺采日不补 0（只补首个样本之前）", async () => {
      const points: StorageTrendPoint[] = [
        { date: "2026-10-05", db_size_kb: 8192, assets_size_kb: 1024 },
        // 10-06 缺整行（后端直接省略）
        { date: "2026-10-07", db_size_kb: null, assets_size_kb: 2048 },
      ];
      const padded = padLeadingDays(points, 5, "2026-10-07");
      assertEqual(padded.length, 4, "只在前面补 3 天，中间缺行不动");
      assertEqual(padded[3]?.db_size_kb, null, "中间 null 保持（断线不断谷）");
    }],

    ["数据已满窗口时不补", async () => {
      const points: StorageTrendPoint[] = [
        { date: "2026-10-06", db_size_kb: 100, assets_size_kb: 100 },
        { date: "2026-10-07", db_size_kb: 200, assets_size_kb: 200 },
      ];
      const padded = padLeadingDays(points, 2, "2026-10-07");
      assertEqual(padded.length, 2, "无需补齐");
      assertEqual(padded[0]?.db_size_kb, 100, "原数据不动");
    }],

    ["空输入不补（组件走空态）", async () => {
      assertEqual(padLeadingDays([], 30, "2026-10-07").length, 0, "空进空出");
    }],
  ],
};

export default suite;
