<template>
  <div class="flex flex-col gap-8">
    <!-- Empty state: with no kids at all, the home page shows no dashboard and
         guides the user straight to creating a profile. No redirect — a redirect
         would make "click home" jump to another page for no reason and look like
         a misclick. -->
    <EmptyState
      v-if="showEmpty"
      icon="person_add"
      title="还没有学生档案"
      description="先添加一个孩子，首页就会显示他的试卷归档与分析面板。"
      action-label="添加第一个孩子"
      @action="goCreateKid"
    />

    <template v-else>
      <ErrorState v-if="error" :message="error" @retry="load" />

      <div
        v-else-if="dashboard"
        class="flex flex-col gap-8"
      >
        <DashboardPanels
          :data="dashboard"
          :kid-id="dashboard.kid.id"
          :trend-days="trendDays"
          :all-kids="allKids"
          @update:trend-days="onTrendDays"
        />

        <!-- ═══ ROW 5 · System monitoring (parent-only) ═══ -->
        <section
          data-testid="system-monitor"
          class="bg-card text-card-foreground flex flex-col gap-4 rounded-xl border py-4 shadow-sm"
        >
          <div class="flex items-center gap-2 px-4 text-base font-medium">
            <Icon name="settings" :size="20" />
            系统运行监控
          </div>
          <div class="px-4">
            <Table :columns="healthColumns" :data="healthRows">
              <!-- Status uses only a dot: green=ok / yellow=warning / red=error.
                   No text (compact); title + aria-label keep accessibility and
                   hover explanation. -->
              <template #cell-status="{ value }">
                <span
                  class="inline-block size-2 rounded-full"
                  :class="statusDotClass(String(value))"
                  :title="statusLabel(String(value))"
                  :aria-label="statusLabel(String(value))"
                  role="img"
                />
              </template>
            </Table>
          </div>
          <!-- Mobile: persistent accent chips; desktop: transparent until hover. -->
          <div class="px-4 text-xs text-muted-foreground flex items-center gap-4 justify-center">
            <LegendChip dot-class="bg-success">正常</LegendChip>
            <LegendChip dot-class="bg-warning">警告</LegendChip>
            <LegendChip dot-class="bg-destructive">错误</LegendChip>
          </div>
        </section>
      </div>

      <p v-else class="text-muted-foreground text-sm">加载中…</p>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { storeToRefs } from "pinia";
import DashboardPanels from "../components/DashboardPanels.vue";
import LegendChip from "../components/LegendChip.vue";
import EmptyState from "../components/EmptyState.vue";
import ErrorState from "../components/ErrorState.vue";
import Icon from "../components/Icon.vue";
import StatusBadge from "../components/StatusBadge.vue";
import Table, { type TableColumn } from "../components/Table.vue";
import { dashboardApi } from "../api/dashboard";
import { metricsApi } from "../api/metrics";
import { useKidStore } from "../stores/kid";
import type { Dashboard, KidPersistence, MetricsSummary } from "../api/types";

/**
 * HomePage: the parent home page (`/`).
 *
 * ROW 1~4 via DashboardPanels; the first three KPI cards carry their own filter,
 * the rest follows the currently selected kid. ROW 5 system monitoring is
 * parent-only. The kid list comes from kidStore (no re-query here).
 */
const router = useRouter();
const kidStore = useKidStore();
const { selectedId, list } = storeToRefs(kidStore);

const trendDays = ref(30);
const dashboard = ref<Dashboard | null>(null);
const error = ref("");
const metrics = ref<MetricsSummary | null>(null);
// Multi-kid persistence series; null = not loaded/unsupported (ROW2 falls back to single-kid).
const allKids = ref<KidPersistence[] | null>(null);

/**
 * Show the empty state only when loading finished with no kids: test list, not
 * selectedId — selectedId is also null mid-load and would flash the empty state.
 */
const showEmpty = computed(() => !kidStore.loading && list.value.length === 0);

const healthColumns: TableColumn[] = [
  { key: "title", title: "检查项" },
  { key: "status", title: "状态", class: "w-20" },
  { key: "value", title: "值", class: "w-32" },
  { key: "detail", title: "说明" },
];

const healthRows = computed<Record<string, unknown>[]>(
  () => (metrics.value?.health as unknown as Record<string, unknown>[]) ?? [],
);

/**
 * Health status -> dot color. Unknown statuses get muted gray, never "ok" — a new
 * backend value must not silently show green.
 */
function statusDotClass(status: string): string {
  switch (status) {
    case "ok":
      return "bg-success";
    case "warn":
      return "bg-warning";
    case "error":
    case "fail":
      return "bg-destructive";
    default:
      return "bg-muted-foreground/40";
  }
}

/** Dot label (hover title + aria-label), no visual space. */
function statusLabel(status: string): string {
  switch (status) {
    case "ok":
      return "正常";
    case "warn":
      return "警告";
    case "error":
    case "fail":
      return "错误";
    default:
      return status;
  }
}

function goCreateKid(): void {
  void router.push({ name: "kids" });
}

function onTrendDays(days: number): void {
  trendDays.value = days;
}

async function load(): Promise<void> {
  error.value = "";
  // No kid selected: the backend requires kid_id, so skip (would only 400).
  if (selectedId.value === null) {
    dashboard.value = null;
    allKids.value = null;
    return;
  }
  try {
    dashboard.value = await dashboardApi.byKid(selectedId.value, trendDays.value);
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
    dashboard.value = null;
  }
  void loadPersistence();
}

/**
 * loadPersistence: parent ROW2 multi-kid persistence (independent of byKid).
 *
 * Failures deliberately don't error: an older backend may lack the endpoint, and
 * falling back to single-kid beats turning the page red. Empty array likewise
 * counts as not loaded.
 */
async function loadPersistence(): Promise<void> {
  try {
    const items = await dashboardApi.persistence(trendDays.value);
    // Array.isArray guards the shape: an older backend may return something unrecognized;
    // never feed malformed values to the chart.
    allKids.value = Array.isArray(items) && items.length > 0 ? items : null;
  } catch {
    allKids.value = null;
  }
}

async function loadMetrics(): Promise<void> {
  try {
    metrics.value = await metricsApi.summary();
  } catch {
    // A failed monitor fetch must not error the page (ROW 1~4 are the content);
    // deliberately no error.value.
    metrics.value = null;
  }
}

watch([selectedId, trendDays], () => void load());

onMounted(async () => {
  if (list.value.length === 0) {
    await kidStore.fetchList();
  }
  void load();
  void loadMetrics();
});
</script>
