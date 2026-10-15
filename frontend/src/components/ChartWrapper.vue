<template>
  <div class="bg-card text-card-foreground flex flex-col gap-6 rounded-xl border py-4 shadow-sm">
    <div class="flex flex-col gap-3 px-4 sm:flex-row sm:items-start sm:justify-between">
      <div class="flex min-w-0 flex-1 flex-col gap-1">
        <div class="flex items-center gap-2 text-base font-medium">
          <Icon v-if="icon" :name="icon" :size="20" />
          {{ title }}
        </div>
        <div v-if="description" class="text-muted-foreground text-sm">{{ description }}</div>
      </div>
      <div v-if="$slots.toolbar" class="flex flex-wrap items-center gap-2">
        <slot name="toolbar" />
      </div>
    </div>
    <div class="min-w-0 px-6">
      <div :style="{ height: `${height}px` }" class="w-full">
        <Line :data="data" :options="options" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
// Must use chart.js/auto for full registration:
// After tree-shaking Chart.js v4, the named-exported controller modules only contain types, not
// class instances (Element/Dataset are the real implementation); calling Chart.register(BarElement)
// on its own yields `"bar" is not a registered controller` (verified under happy-dom).
// The cost is no tree-shaking (chart.js minified ≈ 200KB inside a ≈ 1.2MB total app JS bundle).
import { Bar, Line } from "vue-chartjs";
import "chart.js/auto";
import Icon, { type IconName } from "./Icon.vue";
import { buildLineData, buildLineOptions, buildChartTheme } from "../theme/chart";
import { useThemeMode } from "../theme/mode";

/**
 * ChartWrapper -- Chart.js card wrapper; theme changes rebuild data/options when mode switches.
 */
export interface ChartSeries {
  name: string;
  data: number[];
}

const props = withDefaults(
  defineProps<{
    title: string;
    description?: string;
    icon?: IconName;
    categories: string[];
    series: ChartSeries[];
    height?: number;
  }>(),
  { height: 320 },
);

const { resolved } = useThemeMode();

// Mode switch: CSS variables already updated with <html>; Chart.js has no setOption(merge),
// so swap the whole object and let it diff.
const theme = computed(() => buildChartTheme(resolved.value));
const data = computed(() => buildLineData(theme.value, props.categories, props.series));
const options = computed(() => buildLineOptions(theme.value));
</script>
