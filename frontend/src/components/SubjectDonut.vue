<template>
  <div class="bg-card text-card-foreground flex flex-col gap-6 rounded-xl border py-4 shadow-sm">
    <div class="flex items-start justify-between gap-3 px-4">
      <div class="flex min-w-0 flex-col gap-1">
        <div class="flex items-center gap-2 text-base font-medium">
          <Icon v-if="icon" :name="icon" :size="20" />
          {{ title }}
        </div>
        <div v-if="description" class="text-muted-foreground text-sm">{{ description }}</div>
      </div>
      <!-- Title-row right action slot; no gap when not passed -->
      <div v-if="$slots.action" class="shrink-0">
        <slot name="action" />
      </div>
    </div>

    <!-- Error first: failure and genuine no-data must not both render as no-data -->
    <div v-if="error" class="px-6">
      <ErrorState :message="error" @retry="emit('retry')" />
    </div>

    <!-- Empty state: no canvas (Chart.js draws a broken-looking blank ring on all-zero data) -->
    <div v-else-if="hasData" class="px-6">
      <div :style="{ height: `${height}px` }" class="w-full">
        <Doughnut :data="data" :options="options" />
      </div>
    </div>
    <div v-else class="text-muted-foreground flex h-40 items-center justify-center px-6 text-sm">
      暂无归档数据
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { Doughnut } from "vue-chartjs";
// Must use chart.js/auto for full registration (see the pitfall note in ChartWrapper.vue):
// named-exported controller modules, after tree-shaking, only contain types, not class instances.
import "chart.js/auto";
import type { TooltipItem } from "chart.js";
import ErrorState from "./ErrorState.vue";
import Icon, { type IconName } from "./Icon.vue";
import { assignSubjectColors, buildChartTheme, DOT_SIZE } from "../theme/chart";
import { useThemeMode } from "../theme/mode";
import type { SubjectShare } from "../api/types";

/**
 * SubjectDonut -- subject distribution donut chart.
 *
 * Separate from ChartWrapper (line chart): donut data/config differ too much (no axes,
 * cutout, per-segment colors) to share without if/else. Pure presentational; filters
 * come through the `action` slot.
 */
const props = withDefaults(
  defineProps<{
    title: string;
    description?: string;
    icon?: IconName;
    items: SubjectShare[];
    height?: number;
    /** Non-empty error shows the error state with retry (one card's failure never spills). */
    error?: string;
  }>(),
  { height: 260, error: "" },
);

const emit = defineEmits<{ retry: [] }>();

const { resolved } = useThemeMode();

const hasData = computed(() => props.items.some((i) => i.count > 0));

const theme = computed(() => buildChartTheme(resolved.value));

const data = computed(() => {
  // One color per subject by subject_id rank (see assignSubjectColors); beyond palette capacity still no collisions.
  const colorMap = assignSubjectColors(
    props.items.map((i) => i.subject_id),
    theme.value.palette,
  );
  return {
    labels: props.items.map((i) => i.name),
    datasets: [
      {
        data: props.items.map((i) => i.count),
        backgroundColor: props.items.map(
          (i) => colorMap.get(i.subject_id) ?? "currentColor",
        ),
        borderWidth: 0,
        // Leave a 2px gap so slices don't stick together
        spacing: 2,
      },
    ],
  };
});

const options = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  animation: { duration: 300 },
  cutout: "62%",
  plugins: {
    // Legend comes from the template; switch off Chart.js's to avoid duplication
    legend: { display: false },
    tooltip: {
      backgroundColor: theme.value.tooltipBg,
      borderColor: theme.value.tooltipBorder,
      borderWidth: 1,
      titleColor: theme.value.tooltipText,
      bodyColor: theme.value.tooltipText,
      titleFont: { size: 12 },
      bodyFont: { size: 12 },
      // Hover indicator: solid dot, not the default square; diameter aligned with the trend chart (see DOT_SIZE in chart.ts).
      usePointStyle: true,
      boxWidth: DOT_SIZE,
      boxHeight: DOT_SIZE,
      callbacks: {
        // Fill the dot with the slice color (the default bordered square looks washed out).
        labelColor: (item: TooltipItem<"doughnut">) => {
          const bg = item.chart.data.datasets[item.datasetIndex]?.backgroundColor;
          const color = Array.isArray(bg)
            ? String(bg[item.dataIndex] ?? theme.value.legendText)
            : String(bg ?? theme.value.legendText);
          return { borderColor: color, backgroundColor: color, borderWidth: 1, borderRadius: 0 };
        },
      },
    },
  },
}));
</script>
