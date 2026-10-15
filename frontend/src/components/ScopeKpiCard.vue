<template>
  <KpiCard
    :title="title"
    :icon="icon"
    :value="value"
    :subtext="subtext"
    :loading="loading"
    :error="error"
    @retry="load"
  >
    <template #action>
      <KidScopeFilter v-model="kidId" />
    </template>
  </KpiCard>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import KpiCard from "./KpiCard.vue";
import KidScopeFilter from "./KidScopeFilter.vue";
import { dashboardApi } from "../api/dashboard";
import { deltaPercent } from "../lib/date";
import { avgPagesPerExam } from "../lib/format";
import type { IconName } from "./Icon.vue";
import type { ScopeCounts } from "../api/types";

/**
 * ScopeKpiCard -- the first three KPI cards on the parent home page
 * (self-fetching + card-level filter).
 *
 * Defaults to "all kids"; switching re-fetches only this card (standalone
 * GET /api/dashboard/counts, kid_id optional). The kid side doesn't use this
 * (no filter; feeds dashboard data directly).
 */
type KpiMetric = "total" | "this_month" | "pages";

const props = defineProps<{ icon: IconName; title: string; metric: KpiMetric }>();

const kidId = ref<number | null>(null);
const counts = ref<ScopeCounts | null>(null);
const loading = ref(true);
const error = ref("");

async function load(): Promise<void> {
  error.value = "";
  loading.value = true;
  try {
    counts.value = await dashboardApi.scopeCounts(kidId.value ?? undefined);
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
    counts.value = null;
  } finally {
    loading.value = false;
  }
}

watch(kidId, () => void load());
onMounted(load);

const value = computed(() => {
  const c = counts.value;
  if (!c) return 0;
  if (props.metric === "this_month") return c.exam_this_month;
  if (props.metric === "pages") return c.exam_pages;
  return c.exam_total;
});

const subtext = computed(() => {
  const c = counts.value;
  if (!c) return "";
  if (props.metric === "total") return `本月新增 ${c.exam_this_month}`;
  if (props.metric === "this_month") {
    // No growth rate when last month was 0 (0->0 divides by zero).
    if (c.exam_last_month === 0) return "上月无归档";
    const pct = deltaPercent(c.exam_this_month, c.exam_last_month);
    if (pct === null) return "";
    return `较上月 ${pct > 0 ? "+" : ""}${pct.toFixed(0)}%`;
  }
  // pages: average pages per exam; hide the ratio when the total is 0.
  if (c.exam_total === 0) return "尚无归档";
  return avgPagesPerExam(c.exam_pages, c.exam_total);
});
</script>
