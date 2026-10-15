/**
 * Storage-growth trend line data assembly.
 *
 * Pure functions, not inline in the component: one place for the "two lines, gap as
 * break, KB to MB" rules, unit-testable without mounting.
 */
import type { StorageTrendPoint } from "../api/types";
import type { ChartSeriesInput } from "../theme/chart";

/** KB to MB, 2 decimals; null passes through (gap as break, never 0). */
function kbToMB(kb: number | null): number | null {
  if (kb === null) return null;
  return Math.round(kb / 10.24) / 100;
}

export interface StorageLineSeries {
  labels: string[];
  series: ChartSeriesInput[];
}

/** Local YYYY-MM-DD (the backend trend dates are calendar-day strings in the same zone). */
export function todayLocal(now: Date = new Date()): string {
  const mm = String(now.getMonth() + 1).padStart(2, "0");
  const dd = String(now.getDate()).padStart(2, "0");
  return `${now.getFullYear()}-${mm}-${dd}`;
}

/** Shift a YYYY-MM-DD string by whole calendar days (UTC arithmetic: DST-safe). */
function shiftDate(iso: string, delta: number): string {
  const [y, m, d] = iso.split("-").map(Number);
  const t = Date.UTC(y ?? 1970, (m ?? 1) - 1, d ?? 1) + delta * 86400000;
  const dt = new Date(t);
  const mm = String(dt.getUTCMonth() + 1).padStart(2, "0");
  const dd = String(dt.getUTCDate()).padStart(2, "0");
  return `${dt.getUTCFullYear()}-${mm}-${dd}`;
}

/**
 * Prepend zero-valued days so a young system still draws a full-width axis.
 *
 * Only calendar days **before the first sample** are padded with 0 (before the
 * system existed there was nothing stored, so 0 is truthful, not a forged valley).
 * Missing days on/after the first sample are left untouched: middle gaps keep
 * their existing treatment (omitted rows stay omitted, null fields stay null and
 * render as breaks). Empty input stays empty (the card shows its empty state).
 *
 * `today` is a parameter (not read inside) so tests can pin it deterministically.
 */
export function padLeadingDays(
  points: StorageTrendPoint[],
  days: number,
  today: string,
): StorageTrendPoint[] {
  if (points.length === 0 || days <= 0) return points;
  const first = points[0]?.date ?? "";
  if (first === "") return points;
  const start = shiftDate(today, -(days - 1));
  const out: StorageTrendPoint[] = [];
  for (let d = start; d < first && d <= today; d = shiftDate(d, 1)) {
    out.push({ date: d, db_size_kb: 0, assets_size_kb: 0 });
  }
  return [...out, ...points];
}

/**
 * Converts backend point sequences into line-chart input.
 *
 * - Labels use "MM-DD": full dates overcrowd a 30-day axis, and short
 *   month-day pairs work with Chart.js built-in tick thinning;
 * - The two lines are fixed ("database" and "file assets") in palette order
 *   chart-1/chart-2 (distinct colors; only these two lines exist here, so no
 *   per-subject coloring); legend text carries no units (KB to MB is a unit
 *   conversion, not a semantic change);
 * - Each line's data matches labels in length, with null for gap days.
 */
export function toLineSeries(points: StorageTrendPoint[]): StorageLineSeries {
  const labels = points.map((p) => p.date.slice(5));
  return {
    labels,
    series: [
      { name: "数据库", data: points.map((p) => kbToMB(p.db_size_kb)) },
      { name: "文件资源", data: points.map((p) => kbToMB(p.assets_size_kb)) },
    ],
  };
}
