import type { Chart, TooltipItem } from "chart.js";

/**
 * Chart.js theme factory — reads the current mode's CSS variables from tokens.css.
 *
 * Conventions: tension 0.35, borderWidth 2, curves never filled; dashed grid, hidden
 * verticals; hidden axis border/ticks, font 12; axis/grid colors from muted/border;
 * legend + hover are solid dots from the curve's borderColor (never the area band's
 * translucency — that would yield pale dots).
 */

export type ResolvedTheme = "light" | "dark";

/** Read a CSS variable (current mode value on <html>). */
function cssVar(name: string): string {
  if (typeof document === "undefined") return "";
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

export function chartPalette(_theme?: ResolvedTheme): string[] {
  return [
    cssVar("--chart-1"),
    cssVar("--chart-2"),
    cssVar("--chart-3"),
    cssVar("--chart-4"),
    cssVar("--chart-5"),
  ];
}

/**
 * Line-series color ceiling: multi-kid overlays must not repeat colors. Design palette
 * stays at 5; golden-angle oklch hues extend to 12 before repeating.
 */
export const LINE_SERIES_COLOR_LIMIT = 12;

/**
 * expandPalette pads the palette to the ceiling: first entries kept as-is, rest are
 * golden-angle hues (same generator as assignSubjectColors). Over-limit palettes
 * return unchanged.
 */
export function expandPalette(palette: string[], limit: number = LINE_SERIES_COLOR_LIMIT): string[] {
  if (palette.length >= limit) return palette;
  const out = [...palette];
  for (let rank = palette.length; rank < limit; rank++) {
    const hue = Math.round((rank * 137.508) % 360);
    out.push(`oklch(0.65 0.18 ${hue})`);
  }
  return out;
}

export interface ChartTheme {
  palette: string[];
  gridLineColor: string;
  axisTextColor: string;
  tooltipBg: string;
  tooltipBorder: string;
  tooltipText: string;
  legendText: string;
  tension: number;
  borderWidth: number;
}

export function buildChartTheme(_theme: ResolvedTheme): ChartTheme {
  return {
    palette: chartPalette(_theme),
    gridLineColor: cssVar("--border"),
    axisTextColor: cssVar("--muted-foreground"),
    tooltipBg: cssVar("--popover"),
    tooltipBorder: cssVar("--border"),
    tooltipText: cssVar("--popover-foreground"),
    legendText: cssVar("--muted-foreground"),
    tension: 0.35,
    borderWidth: 2,
  };
}

/**
 * assignSubjectColors assigns stable, mutually distinct colors to subjects.
 *
 * Old code used subject_id % palette length: beyond 5 subjects colors repeat (a mid-exam
 * set has ~10) and sectors blur together. Now rank by subject_id (stable across data
 * reorders, so colors never jump): design palette first, golden-angle oklch hues beyond.
 */
export function assignSubjectColors(
  subjectIds: number[],
  palette: string[],
): Map<number, string> {
  const map = new Map<number, string>();
  // Deduplicate then sort by subject_id (rank = color index).
  const sorted = Array.from(new Set(subjectIds)).sort((a, b) => a - b);
  sorted.forEach((subjectId, rank) => {
    const base = palette[rank];
    if (rank < palette.length && base) {
      map.set(subjectId, base);
    } else {
      // Golden-angle hue rotation: maximal adjacent difference, collision-free.
      const hue = Math.round((rank * 137.508) % 360);
      map.set(subjectId, `oklch(0.65 0.18 ${hue})`);
    }
  });
  return map;
}

export interface ChartSeriesInput {
  name: string;
  /** null = missing sample: line breaks, never pads 0. */
  data: (number | null)[];
}

/**
 * withAlpha appends transparency to a CSS color — chart tokens are oklch(...) (hex in
 * comments is only reference), so use Color 4 `/ <alpha>` syntax; #rrggbb appends two
 * hex digits. Unrecognized formats return unchanged (solid beats invalid).
 */
export function withAlpha(color: string, alpha: number): string {
  const c = color.trim();
  if (!c) return c;
  if (/^#[0-9a-fA-F]{6}$/.test(c)) {
    const hex = Math.round(alpha * 255)
      .toString(16)
      .padStart(2, "0");
    return `${c}${hex}`;
  }
  const m = /^([a-z]+)\(([^)]*)\)$/i.exec(c);
  const name = m?.[1];
  const rawBody = m?.[2];
  if (name !== undefined && rawBody !== undefined) {
    const body = rawBody.includes("/") ? rawBody.slice(0, rawBody.indexOf("/")) : rawBody;
    // Replace commas with spaces so rgb(1,2,3) can also be assembled into Color 4's rgb(1 2 3 / 0.08)
    return `${name}(${body.replace(/,/g, " ")} / ${alpha})`;
  }
  return c;
}

/**
 * AREA_FILL_ALPHA is the area band transparency: 22% — the curve is the protagonist, the band is
 * clearly distinguishable for volume outline without overpowering the curve (the storage-growth
 * card and the persistence line share the same style value). Only takes effect when the caller
 * explicitly opts in.
 */
export const AREA_FILL_ALPHA = 0.22;

/**
 * DOT_SIZE — the dot diameter (px) for legend / hover indicator.
 *
 * **Keeps the same gauge** as the legend dot atop the storage-growth card (the 7px solid-color
 * dot with `r = 3.5` in StorageTrendCard): within one dashboard, two small widgets of mismatched
 * size would look like a bug.
 *
 * Chart.js dot-size conversion is inconsistent; the pitfalls are these two formulas:
 *   - tooltip: radius = min(boxWidth, boxHeight) / 2;
 *   - legend (with usePointStyle): radius = boxHeight * √2 / 2
 *     (it draws the dot inscribed in the box diagonal, see getBoxSize / element.legend's
 *     drawLegendBox). So to get 7px in the legend, boxHeight must be 7/√2 ≈ 4.95 —
 *     writing 4.95 directly in code is a magic value, so here the intent is expressed as a formula.
 */
export const DOT_SIZE = 7;
export const LEGEND_BOX_HEIGHT = DOT_SIZE / Math.SQRT2;

/**
 * buildLineData turns theme + categories + series into a Chart.js data structure.
 *
 * Pure curves (fill: false) are the default: for "trend" metrics like persistence you look at
 * the trend, and filled blocks make people mistake it for comparing area sizes while also making
 * multiple overlapping curves obscure and blur each other's edges.
 * Cards wanting an area style explicitly opt in: fill: "origin" + withAlpha(same color, AREA_FILL_ALPHA).
 * pointRadius 0 only grows on hover (pointHoverRadius), keeping the line clean.
 */
export function buildLineData(
  theme: ChartTheme,
  categories: string[],
  series: ChartSeriesInput[],
): { labels: string[]; datasets: Record<string, unknown>[] } {
  // Expanded palette: 12 distinct line colors (5 design + golden-angle supplements) so a
  // 6th kid does not silently reuse the 1st kid's color; % still caps the true ceiling.
  const palette = expandPalette(theme.palette);
  return {
    labels: categories,
    datasets: series.map((s, i) => {
      const color = palette[i % palette.length] || cssVar("--chart-1");
      return {
        label: s.name,
        data: s.data,
        borderColor: color,
        borderWidth: theme.borderWidth,
        tension: theme.tension,
        fill: false,
        pointRadius: 0,
        pointHoverRadius: 4,
      };
    }),
  };
}

/**
 * buildLineOptions generates common Chart.js options (axes / grid / tooltip / legend).
 * Chart.js axes live under scales, tooltip/legend are top-level keys — unlike ECharts'
 * structure, hence assembled here so pages only pass data.
 */
export function buildLineOptions(theme: ChartTheme): Record<string, unknown> {
  const axis = {
    border: { display: false },
    grid: { display: false },
    ticks: { color: theme.axisTextColor, font: { size: 12 }, padding: 8 },
  };
  return {
    responsive: true,
    maintainAspectRatio: false,
    animation: { duration: 300 },
    interaction: { mode: "index" as const, intersect: false },
    plugins: {
      legend: {
        position: "bottom" as const,
        labels: {
          color: theme.legendText,
          font: { size: 13 },
          padding: 16,
          usePointStyle: true,
          pointStyle: "circle",
          // Legend dot diameter aligned with the storage-growth card (see the DOT_SIZE formula comment):
          // boxHeight is the only knob determining the usePointStyle dot radius
          // (radius = boxHeight*√2/2), boxWidth only affects the item-layout width, not the dot.
          boxHeight: LEGEND_BOX_HEIGHT,
          boxWidth: DOT_SIZE,
          generateLabels: (chart: Chart<"line">) => {
            // The default legend item fillStyle takes dataset.backgroundColor — for an area card
            // it is a translucent band, so the dot would look pale; here it is uniformly changed
            // to the curve's full borderColor (a solid dot).
            // Also must explicitly set fontColor: drawLegend paints text with ctx.fillStyle = fontColor,
            // and without it the text reuses the previous frame's canvas fill (persistent white-on-dark text).
            return chart.data.datasets.map((ds, i) => {
              const color =
                (typeof ds.borderColor === "string" && ds.borderColor) ||
                theme.legendText;
              return {
                text: ds.label ?? "",
                fillStyle: color,
                strokeStyle: color,
                lineWidth: 1,
                hidden: !chart.isDatasetVisible(i),
                pointStyle: "circle" as const,
                rotation: 0,
                fontColor: theme.legendText,
                datasetIndex: i,
                index: i,
              };
            });
          },
        },
      },
      tooltip: {
        backgroundColor: theme.tooltipBg,
        borderColor: theme.tooltipBorder,
        borderWidth: 1,
        titleColor: theme.tooltipText,
        bodyColor: theme.tooltipText,
        titleFont: { size: 12 },
        bodyFont: { size: 12 },
        // Hover indicator changed from the default "colored square" to a solid dot (boxWidth+2 = 14px keeps natural spacing from text)
        usePointStyle: true,
        // Dot diameter matches the legend (radius = min(boxWidth, boxHeight)/2 → 7/2 = 3.5px
        // radius = same 7px dot as the storage-growth card).
        boxWidth: DOT_SIZE,
        boxHeight: DOT_SIZE,
        // Slightly loosen vertical row spacing: the default bodySpacing=2 is too tight at 12px
        // font, in a multi-kid trend two rows squeeze into one horizontal band and become illegible.
        bodySpacing: 6,
        callbacks: {
          labelColor: (tooltipItem: TooltipItem<"line">) => {
            const ds = tooltipItem.chart.data.datasets[tooltipItem.datasetIndex];
            const color =
              (typeof ds?.borderColor === "string" && ds.borderColor) ||
              theme.legendText;
            return { borderColor: color, backgroundColor: color, borderWidth: 1, borderRadius: 0 };
          },
        },
      },
    },
    scales: {
      x: { ...axis, stacked: false },
      y: {
        ...axis,
        stacked: false,
        beginAtZero: true,
        grid: {
          color: theme.gridLineColor,
          borderDash: [4, 4],
          drawTicks: false,
        },
      },
    },
  };
}
