<template>
  <div class="flex flex-col gap-8">
    <!-- === ROW 1 - core statistics === -->
    <!-- The first three cards on the parent side self-fetch counts for "all / one kid", each with its
          own filter; on the kid side the dashboard data is already kid-scoped, so use it directly (no filter).
          The 4th card is the storage growth trend (two-line chart): volume has no kid/all distinction
          (a Box machine shares one data directory), so both sides show the same card. -->
    <!-- Four cards per row on desktop, two rows (2x2) on mobile. Use lg not xl:
          only laying out four above 1280 would keep common 1024~1280 laptops at 2x2,
          while the four cards do fit at that width. -->
    <section class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
      <!-- Parent side: the first three self-fetch + card-level "all / one kid" filter (single count).
            Kid side: dashboard data is already kid-scoped, no filter, single count. -->
      <template v-if="isKid">
        <KpiCard
          icon="description"
          title="归档试卷总数"
          :value="data.exams.total"
          :subtext="`本月新增 ${data.exams.this_month}`"
        />
        <KpiCard
          icon="upload"
          title="本月新增试卷"
          :value="data.exams.this_month"
          :subtext="monthDeltaText"
        />
        <KpiCard
          icon="picture_as_pdf"
          title="电子化总页数"
          :value="data.exam_pages"
          :subtext="pageSubtext"
        />
      </template>
      <template v-else>
        <ScopeKpiCard icon="description" title="归档试卷总数" metric="total" />
        <ScopeKpiCard icon="upload" title="本月新增试卷" metric="this_month" />
        <ScopeKpiCard icon="picture_as_pdf" title="电子化总页数" metric="pages" />
      </template>
      <!-- 4th card: the former "pending background tasks" is replaced by the storage growth trend.
           Task state is still visible in ROW5 health checks and the settings page, whereas "are the
           DB/assets growing" had no answer before. -->
      <StorageTrendCard />
    </section>

    <!-- === ROW 2 - persistence (narrowed to kid) === -->
    <section
      class="bg-card text-card-foreground flex flex-col gap-2 rounded-xl border py-4 shadow-sm"
    >
      <div class="flex items-center justify-between gap-3 px-4">
        <div class="flex min-w-0 flex-1 items-center gap-2 text-base font-medium">
          <Icon name="show_chart" :size="20" class="shrink-0" />
          <span class="truncate">{{ row2Title }}</span>
        </div>
        <!-- 7D / 30D only change the chart window. Always on the same row as the
             title (right-aligned), never its own row — even on narrow screens the
             title truncates first via min-w-0. -->
        <div class="flex shrink-0 items-center gap-1 rounded-lg border p-0.5">
          <button
            v-for="w in TREND_WINDOWS"
            :key="w"
            type="button"
            class="rounded-md px-2.5 py-1 text-xs transition-colors"
            :class="
              w === trendDays
                ? 'bg-secondary text-secondary-foreground font-medium'
                : 'text-muted-foreground hover:text-foreground'
            "
            @click="emit('update:trendDays', w)"
          >
            {{ w }}d
          </button>
        </div>
      </div>

      <div class="flex flex-col gap-1 px-4">
        <div class="h-52 w-full">
          <Line :data="lineData" :options="lineOptions" />
        </div>
        <!-- Kid-name legend as DOM chips instead of the Chart.js canvas legend: same
             chip pattern as every other legend in the app (mobile accent chips, desktop
             hover), flowing left-to-right and wrapping when the row runs out of room.
             Dots keep the curve palette colors. Shown whenever there is at least one
             series: a parent with a single active kid gets the multi-kid "上传坚持度"
             title with one line, and that line must still carry its key. -->
        <div
          v-if="lineLegend.length > 0"
          data-testid="persistence-legend"
          class="text-muted-foreground flex flex-wrap items-center justify-center gap-x-2 gap-y-2.5 text-xs"
        >
          <LegendChip v-for="it in lineLegend" :key="it.name" :dot-color="it.color">{{
            it.name
          }}</LegendChip>
        </div>
      </div>
    </section>

    <!-- === ROW 3 - analysis and placeholder ===
         The kid side shows it **as well**: the exam distribution and radar placeholder are the main
         content of "my learning space", and hiding them would leave the kid's home page with only a
         trend line. The historical exam table (ROW 4) is shown on both sides, and the kid side uses
         its own /kid/exams route for the title, row links and "more". -->
    <section class="grid grid-cols-1 gap-4 sm:grid-cols-2">
      <!-- Parent side: standalone fetch + card-level "all / one kid" filter (does not follow the top bar).
            Kid side: still uses the kid-narrowed distribution from dashboard, no filter. -->
      <DistributionCard v-if="!isKid" />

      <SubjectDonut
        v-else
        icon="pie_chart"
        title="我的试卷分布"
        :items="data.distribution"
      />

      <!-- Phase II placeholder: fake gray radar showing the intended shape; real data
           wires in when the knowledge-point analysis lands. Gray (muted-foreground)
           signals "not real data" — never a palette color, which would read as a
           finished analysis. -->
      <div
        data-testid="radar-card"
        class="hidden bg-card text-card-foreground flex-col gap-4 rounded-xl border-2 border-dashed py-4 shadow-sm sm:flex"
      >
        <div class="flex items-center justify-between gap-2 px-4">
          <div class="flex min-w-0 items-center gap-2 text-base font-medium text-muted-foreground">
            <Icon name="info" :size="20" class="shrink-0" />
            <span class="truncate">雷达图（预留）</span>
            <span class="bg-accent shrink-0 rounded-full border px-2.5 py-1 text-xs font-normal">示例数据</span>
          </div>
          <KidScopeFilter v-if="!isKid" v-model="radarKidId" :allow-all="false" />
        </div>
        <div class="h-64 w-full px-4">
          <Radar :data="radarData" :options="radarOptions" />
        </div>
      </div>
    </section>

    <!-- === ROW 4 - historical exam table ===
         The parent side defaults to all kids and can switch a kid within the card; the kid side is
         locked to itself (kidID passed by the parent; on the kid side it is the kid.id looked up
         from dashboard). -->
    <ExamTableCard :kid-id="kidId" :kid="isKid" />
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Line, Radar } from "vue-chartjs";
import "chart.js/auto";
import DistributionCard from "./DistributionCard.vue";
import Icon from "./Icon.vue";
import ExamTableCard from "./ExamTableCard.vue";
import KpiCard from "./KpiCard.vue";
import KidScopeFilter from "./KidScopeFilter.vue";
import LegendChip from "./LegendChip.vue";
import ScopeKpiCard from "./ScopeKpiCard.vue";
import StorageTrendCard from "./StorageTrendCard.vue";
import SubjectDonut from "./SubjectDonut.vue";
import { subjectApi, type Subject } from "../api/subject";
import { useKidStore } from "../stores/kid";
import {
  AREA_FILL_ALPHA,
  DOT_SIZE,
  buildChartTheme,
  buildLineData,
  buildLineOptions,
  withAlpha,
} from "../theme/chart";
import { useThemeMode } from "../theme/mode";
import { deltaPercent } from "../lib/date";
import { avgPagesPerExam } from "../lib/format";
import { KID_STATUS } from "../api/types";
import type { Dashboard, KidPersistence } from "../api/types";

/**
 * DashboardPanels -- the **single implementation** of home page ROW 1~4.
 *
 * Both sides are structurally identical (each sees only its own kid); two copies
 * would drift into "parent sees ≠ kid sees" bugs nobody looks for. The only
 * difference is where kidID comes from: parent takes the first active kid, the
 * student side looks up access_code. The component only honors passed-in data.
 */
const TREND_WINDOWS = [7, 30] as const;

const props = defineProps<{
  data: Dashboard;
  kidId: number;
  trendDays: number;
  isKid?: boolean;
  /**
   * Parent-side multi-kid persistence series; null/undefined = not loaded or
   * unsupported — ROW2 falls back to data.series' single-kid line (+ name title).
   */
  allKids?: KidPersistence[] | null;
}>();

/**
 * showAllKids -- "kids' persistence" multi-kid overlay mode.
 *
 * Legend covers all active kids regardless of the selected one; a graduated/deleted
 * kid just lacks its area band. Single-kid line + title is only the endpoint-failed
 * fallback.
 */
const showAllKids = computed(
  () =>
    props.allKids !== null &&
    props.allKids !== undefined &&
    props.allKids.length > 0,
);

/** ROW2 title: kid side "my ..."; parent prefers multi-kid overlay,
 *  single-kid fallback on failure. */
const row2Title = computed(() => {
  if (props.isKid) return "我的坚持度";
  if (showAllKids.value) return "上传坚持度";
  return `${props.data.kid.name}的坚持度`;
});

const emit = defineEmits<{ "update:trendDays": [days: number] }>();

const { resolved } = useThemeMode();

// -- ROW 1 subtext ------------------------------------------

/**
 * Month-over-month delta for the "new this month" card. Numerator is this_month, not
 * total (total would compute absurd values like 740%). Hidden when last month is 0.
 */
const monthDeltaText = computed(() => {
  const cur = props.data.exams.this_month;
  const prev = props.data.exams.last_month;
  if (prev === 0) return "上月无归档";
  const pct = deltaPercent(cur, prev);
  if (pct === null) return "";
  return `较上月 ${pct > 0 ? "+" : ""}${pct.toFixed(0)}%`;
});

/** "average 3.0 pages/exam"; hidden when the total is 0 (0/0). */
const pageSubtext = computed(() => {
  const total = props.data.exams.total;
  if (total === 0) return "尚无归档";
  return avgPagesPerExam(props.data.exam_pages, total);
});

// -- ROW 2 --------------------------------------------------

const theme = computed(() => buildChartTheme(resolved.value));

const lineData = computed(() => {
  // Default single kid (data.series); multi-kid overlay groups by kid, zero-filled to one window.
  let categories: string[] = props.data.series.map((p) => p.date);
  let series: { name: string; data: number[] }[] = [
    { name: `我的归档趋势`, data: props.data.series.map((p) => p.count) },
  ];
  if (showAllKids.value && props.allKids !== null) {
    categories = props.allKids[0]!.series.map((p) => p.date);
    series = props.allKids.map((k) => ({ name: k.name, data: k.series.map((p) => p.count) }));
  }

  const data = buildLineData(theme.value, categories, series);
  // Area band (storage-card style) only on the selected kid's line — banding all lines
  // would obscure who missed days.
  return {
    ...data,
    datasets: data.datasets.map((ds, i) => {
      const isHero =
        !showAllKids.value ||
        (props.allKids !== null && props.allKids[i]?.kid_id === props.kidId);
      return {
        ...ds,
        fill: (isHero ? "origin" : false) as "origin" | false,
        backgroundColor: isHero
          ? withAlpha(String(ds.borderColor ?? ""), AREA_FILL_ALPHA)
          : undefined,
      };
    }),
  };
});

// Canvas legend off (template draws DOM chips); tooltip stays from the shared builder.
// Deep-merge the override, don't fork buildLineOptions.
const lineOptions = computed(() => {
  const base = buildLineOptions(theme.value);
  return {
    ...base,
    plugins: { ...(base.plugins as Record<string, unknown>), legend: { display: false } },
  };
});

/** Legend chips mirror datasets (name ← label, dot ← borderColor). */
const lineLegend = computed(() =>
  lineData.value.datasets.map((ds) => ({
    name: String(ds.label ?? ""),
    color: typeof ds.borderColor === "string" ? ds.borderColor : "",
  })),
);

// -- ROW 3 placeholder radar ----------------------------------
// Fake values, gray only (Phase II shape preview); axes are real (the kid's
// kid_subjects whitelist). Card-local switch, same pattern as ExamTableCard —
// never touches the top-bar current kid.
const kidStore = useKidStore();
const activeKids = computed(() => kidStore.list.filter((k) => k.status === KID_STATUS.ACTIVE));

const radarKidId = ref<number | null>(null);
const radarSubjects = ref<Subject[]>([]);

const FAKE_SUBJECT_FALLBACK = ["数学", "语文", "英语", "物理", "化学"];
const FAKE_RADAR_VALUES = [62, 74, 58, 70, 66, 80, 72, 64, 68, 76];

// Heal null/stale scope to the smallest active kid (no "all": axes need one
// kid's kid_subjects).
watch(
  activeKids,
  (kids) => {
    if (props.isKid || kids.length === 0) return;
    if (radarKidId.value === null || !kids.some((k) => k.id === radarKidId.value)) {
      radarKidId.value = Math.min(...kids.map((k) => k.id));
    }
  },
  { immediate: true },
);

// Whitelist owner: kid side itself, parent side the card-local pick.
const radarKidForSubjects = computed(() =>
  props.isKid ? props.kidId : (radarKidId.value ?? 0),
);

// Axes from kid_subjects (grade/track mask fallback — see backend ListForKid). This
// endpoint is readable by both tokens, unlike parent-only /api/kids/:id/subjects.
// Empty falls back to generic placeholder axes, never an empty radar.
watch(
  radarKidForSubjects,
  async (id) => {
    if (id <= 0) {
      radarSubjects.value = [];
      return;
    }
    try {
      radarSubjects.value = (await subjectApi.list(undefined, undefined, id)) ?? [];
    } catch {
      radarSubjects.value = [];
    }
  },
  { immediate: true },
);

const radarLabels = computed(() => {
  const names = radarSubjects.value.map((s) => s.name);
  return names.length > 0 ? names : FAKE_SUBJECT_FALLBACK;
});

const radarData = computed(() => {
  const gray = theme.value.axisTextColor;
  const labels = radarLabels.value;
  return {
    labels,
    datasets: [
      {
        label: "掌握度（示例）",
        data: labels.map((_, i) => FAKE_RADAR_VALUES[i % FAKE_RADAR_VALUES.length] ?? 60),
        borderColor: gray,
        backgroundColor: withAlpha(gray, AREA_FILL_ALPHA),
        pointBackgroundColor: gray,
        pointBorderColor: gray,
        borderWidth: 2,
      },
    ],
  };
});

const radarOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  animation: { duration: 300 },
  plugins: {
    legend: { display: false },
    tooltip: {
      backgroundColor: theme.value.tooltipBg,
      borderColor: theme.value.tooltipBorder,
      borderWidth: 1,
      titleColor: theme.value.tooltipText,
      bodyColor: theme.value.tooltipText,
      titleFont: { size: 12 },
      bodyFont: { size: 12 },
      // Hover marker: solid dot at DOT_SIZE gauge (a square would read as another language).
      usePointStyle: true,
      boxWidth: DOT_SIZE,
      boxHeight: DOT_SIZE,
      bodySpacing: 6,
    },
  },
  scales: {
    r: {
      min: 0,
      max: 100,
      ticks: {
        stepSize: 25,
        color: theme.value.axisTextColor,
        backdropColor: "transparent",
        font: { size: 10 },
      },
      grid: { color: theme.value.gridLineColor },
      angleLines: { color: theme.value.gridLineColor },
      pointLabels: { color: theme.value.axisTextColor, font: { size: 12 } },
    },
  },
}));
</script>
