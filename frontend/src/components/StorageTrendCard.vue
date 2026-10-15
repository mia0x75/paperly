<template>
  <div
    class="bg-card text-card-foreground flex flex-col gap-3 rounded-xl border pt-4 pb-3 shadow-sm"
  >
    <div class="flex items-center gap-2 px-4 text-base font-medium">
      <Icon name="trending_up" :size="20" />
      存储增长
    </div>

    <div v-if="error" class="px-4">
      <ErrorState :message="error" @retry="load" />
    </div>
    <EmptyState
      v-else-if="!loading && points.length === 0"
      icon="folder"
      title="暂无存储趋势"
      description="每日维护运行后开始记录体量快照"
    />
    <!-- Legend chips overlay the canvas' reserved top band (same compact card height as before): the
          upload-persistence chip pattern (mobile accent / desktop hover) kept, but absolutely
          positioned so the card does not grow a legend row -- layout.padding.top reserves the band
          and the curve starts right at its lower edge. -->
    <div v-else class="px-4">
      <div v-if="loading" class="h-18 w-full animate-pulse rounded-md bg-muted" />
      <div v-else class="relative h-18 w-full">
        <div
          v-if="legend.length > 0"
          data-testid="storage-legend"
          class="text-muted-foreground absolute left-0 top-0 z-10 flex flex-wrap items-center gap-x-1.5 gap-y-1.5 text-xs"
        >
          <LegendChip v-for="it in legend" :key="it.name" :dot-color="it.color">{{
            it.name
          }}</LegendChip>
        </div>
        <Line :data="lineData" :options="lineOptions" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { Line } from "vue-chartjs";
import "chart.js/auto";
import EmptyState from "./EmptyState.vue";
import ErrorState from "./ErrorState.vue";
import Icon from "./Icon.vue";
import LegendChip from "./LegendChip.vue";
import { metricsApi } from "../api/metrics";
import { padLeadingDays, toLineSeries, todayLocal } from "../lib/storageTrend";
import {
  AREA_FILL_ALPHA,
  buildChartTheme,
  buildLineData,
  buildLineOptions,
  withAlpha,
} from "../theme/chart";
import { useThemeMode } from "../theme/mode";
import type { StorageTrendPoint } from "../api/types";

/**
 * StorageTrendCard -- 4th Dashboard first-row card: storage growth trend (two-line area).
 * No hover; legend is a DOM chip row (upload-persistence pattern), Chart.js legend/tooltip off.
 *
 * Data source is daily-maintenance gauge snapshots (retained 365 days), not real-time
 * (/metrics/summary answers "now"; this card answers "trend"). Self-fetching, so a
 * failure affects only this card. Shared by parent and kid sides (requireAny).
 */
const DEFAULT_DAYS = 30;

/** Legend band reserved atop the canvas: DOM chips overlay it, so the legend takes no extra card height. */
const LEGEND_H = 24;

const { resolved } = useThemeMode();

const theme = computed(() => buildChartTheme(resolved.value));

const loading = ref(true);
const error = ref("");
const points = ref<StorageTrendPoint[]>([]);

async function load(): Promise<void> {
  loading.value = true;
  error.value = "";
  try {
    const data = await metricsApi.storageTrend(DEFAULT_DAYS);
    // Fall back to an empty series (test stubs may give data:null).
    points.value = data?.points ?? [];
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
    points.value = [];
  } finally {
    loading.value = false;
  }
}

onMounted(load);

const trend = computed(() =>
  // Pad pre-history days with 0: a fresh system still draws a full 30-day axis.
  toLineSeries(padLeadingDays(points.value, DEFAULT_DAYS, todayLocal())),
);

/** Legend: dot takes the first two palette colors (same source as the curve). */
const legend = computed(() =>
  trend.value.series.map((s, i) => ({
    name: s.name,
    color: theme.value.palette[i % theme.value.palette.length] ?? "",
  })),
);

const lineData = computed(() => {
  const data = buildLineData(theme.value, trend.value.labels, trend.value.series);
  return {
    ...data,
    datasets: data.datasets.map((ds, i) => ({
      ...ds,
      // Dual scale: image files grow by MB/day, while a family DB stays ~1-2MB total over
      // 12 years — a shared y would flatten the DB curve against zero. The database line
      // (index 0, fixed order in toLineSeries) gets its own axis "y", files get "y1"; each
      // axis autofits to its own series range (both hidden, see lineOptions) so the DB's
      // slow growth stays a visible slope instead of a zero line.
      // Area chart: fill to origin (clipped to the chart bottom when 0 is outside the visible range), 22% translucent band
      yAxisID: i === 0 ? "y" : "y1",
      fill: "origin",
      backgroundColor: withAlpha(String(ds.borderColor ?? ""), AREA_FILL_ALPHA),
      // Generic is a 0.35 deflection line; this card raises tension for a smoother curve (sharp corners
      // slightly overshoot the actual value, but it's enough to express a trend)
      tension: 0.6,
      pointHoverRadius: 0,
    })),
  };
});

const lineOptions = computed(() => {
  const opts = buildLineOptions(theme.value);
  const plugins = opts.plugins as Record<string, unknown>;
  const scales = opts.scales as Record<string, unknown>;
  return {
    ...opts,
    // Reserve the legend band at top (the overlay chips sit on exactly this band, so the curve
    // follows the legend's lower edge with 0 gap and the card height stays compact)
    layout: { padding: { top: LEGEND_H } },
    // Built-in legend/tooltip disabled (the legend is the DOM chip overlay; this card has no hover interaction)
    plugins: {
      ...plugins,
      legend: { display: false },
      tooltip: { enabled: false },
    },
    // Axes fully hidden; neither y is anchored to 0 (each autofits its own series). One
    // shared scale would collapse the tiny database line under the file-assets values, so
    // the two series map to separate hidden axes (y / y1, see lineData) — the trend stays
    // proportional per curve without the ambiguity of visible dual ticks.
    scales: {
      ...scales,
      x: { display: false },
      y: { display: false, beginAtZero: false },
      y1: { display: false, beginAtZero: false },
    },
  };
});
</script>
