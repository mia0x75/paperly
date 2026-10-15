<template>
  <div class="flex flex-col gap-6">
    <section class="bg-card text-card-foreground flex flex-col gap-4 rounded-xl border p-6 shadow-sm">
      <!-- Filters and "upload exam": two per row on mobile (grid-cols-2; parent has 6
           controls -> 3 rows, kid has 5 -> last row single, right-aligned), one row on lg+. -->
      <div class="grid grid-cols-2 gap-2 lg:flex lg:flex-wrap lg:items-center lg:gap-2">
        <Select
          v-if="!isKidRoute"
          v-model="kidFilter"
          :options="kidOptions"
          class="w-full lg:w-32"
          @change="applyFilters"
        />
        <Select v-model="subjectId" :options="subjectOptions" class="w-full lg:w-36" @change="applyFilters" />
        <Select v-model="grade" :options="gradeOptions" class="w-full lg:w-32" @change="applyFilters" />
        <Select v-model="status" :options="statusOptions" class="w-full lg:w-32" @change="applyFilters" />
        <Button variant="outline" class="w-full lg:w-auto" @click="resetFilters">重置</Button>
        <Button
          variant="outline"
          class="col-start-2 w-full lg:col-auto lg:ml-auto lg:w-auto"
          icon="add"
          @click="router.push(isKidRoute ? '/kid/exams/upload' : '/exams/upload')"
        >
          上传试卷
        </Button>
      </div>

      <ErrorState v-if="error !== ''" :message="error" retry-label="重试" @retry="load" />

      <!-- Table sits directly on the section card (same as settings-page lists):
           section is already a solid bg-card, so no extra card wrapper is needed,
           avoiding two same-colored layers. -->
      <template v-if="loading || items.length > 0">
        <Table :columns="columns" :data="rows">
        <!-- The title is the entry point: click it to open detail (consistent with
             the home page). The trailing column no longer holds a "detail" button,
             since one action occupying a column would eat horizontal space, and
             "click the title to enter" is the more natural habit. -->
        <template #cell-title="{ row }">
          <span class="inline-flex items-center gap-2">
            <!-- Status dot before the name (green=normal, red=deleted); title/aria-label
                 carry the meaning, so no separate status column is needed. -->
            <LegendChip
              bare
              :dot-class="statusDotClass(Number(row.status))"
              :title="statusLabel(Number(row.status))"
              :aria-label="statusLabel(Number(row.status))"
              role="img"
            />
            <RouterLink
              :to="`${isKidRoute ? '/kid/exams' : '/exams'}/${row.id}`"
              class="text-foreground hover:text-foreground font-medium transition-colors no-underline"
              :title="row.title ?? `试卷 #${row.id}`"
            >
              {{ row.title ?? `试卷 #${row.id}` }}
            </RouterLink>
          </span>
        </template>
        <template #cell-subject_id="{ row }">
          {{ subjectName(row.subject_id) }}
        </template>
        <template #cell-grade="{ row }">
          {{ gradeLabelOr(row.grade) }}
        </template>
        <template #cell-exam_type="{ row }">
          {{ examTypeLabel(row.exam_type) }}
        </template>
        <template #cell-exam_date="{ row }">
          {{ row.exam_date ?? "—" }}
        </template>
        <template #cell-kid_id="{ row }">
          {{ kidNameById(kidStore.list, row.kid_id as number | null) }}
        </template>
        <template #cell-page_count="{ row }">
          <span class="tabular-nums">{{ row.page_count }} 页</span>
        </template>
      </Table>
        <!-- Mobile: persistent accent chips; desktop: transparent until hover. -->
        <div class="px-6 text-xs text-muted-foreground flex items-center gap-4 justify-center">
          <LegendChip dot-class="bg-success">正常</LegendChip>
          <LegendChip dot-class="bg-destructive">删除</LegendChip>
        </div>
      </template>
      <EmptyState
        v-else-if="!error"
        icon="description"
        title="暂无试卷"
        description="上传第一份试卷，开始归档与分析。"
        action-label="上传第一份试卷"
        @action="router.push(isKidRoute ? '/kid/exams/upload' : '/exams/upload')"
      />
      <div class="flex flex-wrap items-center justify-between gap-2">
        <span class="text-muted-foreground text-sm">
          <template v-if="loading"> · 加载中…</template>
        </span>
        <div class="flex items-center gap-2">
          <Button variant="outline" size="sm" :disabled="pageNo <= 1 || loading" @click="prevPage">
            上一页
          </Button>
          <Button variant="outline" size="sm" :disabled="!hasMore || loading" @click="nextPage">
            下一页
          </Button>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import Button from "../components/ui/Button.vue";
import EmptyState from "../components/EmptyState.vue";
import ErrorState from "../components/ErrorState.vue";
import LegendChip from "../components/LegendChip.vue";
import Table, { type TableColumn } from "../components/Table.vue";
import Select from "../components/ui/Select.vue";
import { examApi } from "../api/exam";
import { subjectApi, type Subject } from "../api/subject";
import { useKidStore } from "../stores/kid";
import { useKidSessionStore } from "../stores/kidSession";
import { GRADE_OPTIONS, gradeLabelOr } from "../lib/grade";
import { kidNameById } from "../lib/kid";
import { EXAM_TYPE_OPTIONS } from "../lib/examTitle";
import { EXAM_STATUS, type ExamListResponse } from "../api/types";
import type { SelectOption } from "../lib/ui";

/**
 * ExamListPage: exam list (`/exams`).
 *
 * Pagination and filtering are all server-side (GET /api/exams takes
 * cursor/limit/subject_id/grade/exam_type/kid_id/date_from/date_to/status). The
 * front end holds only state, never a full cache. Status is the real two-state
 * exams.status (1=active / 2=deleted).
 */
const PAGE_SIZE = 10;


const router = useRouter();
const route = useRoute();
const kidStore = useKidStore();
const kidSession = useKidSessionStore();

const data = ref<ExamListResponse | null>(null);
const subjects = ref<Subject[]>([]);
const loading = ref(false);
const error = ref("");

/** Cursor pagination: cursorStack[0] is always 0; a stack (not just next_cursor)
 *  supports "previous page" (keyset can't go back). */
const cursorStack = ref<number[]>([0]);
const hasMore = ref(false);
const subjectId = ref(0);
/**
 * Grade filter initial: kid side defaults to the session grade (own exams are
 * almost always own grade); parent side 0 = all.
 */
const grade = ref(route.meta.audience === "kid" ? kidSession.grade : 0);
const status = ref(0);

const subjectOptions = computed<readonly SelectOption<number>[]>(() => [
  { value: 0, label: "科目: 全部" },
  ...subjects.value.map((s) => ({ value: s.id, label: s.name })),
]);
const gradeOptions: readonly SelectOption<number>[] = [
  { value: 0, label: "年级: 全部" },
  ...GRADE_OPTIONS,
];
const statusOptions: readonly SelectOption<number>[] = [
  { value: 0, label: "全部状态" },
  { value: EXAM_STATUS.ACTIVE, label: "正常" },
  { value: EXAM_STATUS.DELETED, label: "已删除" },
];
/**
 * `?kid_id=` from a student-detail deep link seeds the parent-side kid filter.
 * Invalid/missing is null — never send NaN.
 */
const isKidRoute = computed(() => route.meta.audience === "kid");

const routeKidID = ref<number | null>(readKidQuery());

const kidFilter = ref<number>(routeKidID.value ?? 0);

const kidOptions = computed<readonly SelectOption<number>[]>(() => [
  { value: 0, label: "孩子: 全部" },
  ...kidStore.list.map((k) => ({ value: k.id, label: k.name })),
]);

/**
 * The owning kid_id actually sent: kid side always its own session kid; parent side
 * the filter (0 = all, not sent).
 */
const scopeKidId = computed<number | null>(() => {
  if (isKidRoute.value) return routeKidID.value ?? kidSession.kidId;
  return kidFilter.value > 0 ? kidFilter.value : null;
});

const items = computed(() => data.value?.items ?? []);
/** Display-only page number (not a query param). */
const pageNo = computed(() => cursorStack.value.length);

// Table's generic needs Record<string, unknown>; ExamSummary has none — convert here,
// don't touch the domain contract.
const rows = computed<Record<string, unknown>[]>(
  () => items.value as unknown as Record<string, unknown>[],
);

// Ownership column is parent-side only (kid side is always self — marking it would be redundant).
const columns = computed<TableColumn[]>(() => [
  ...(isKidRoute.value ? [] : [{ key: "kid_id", title: "归属", class: "w-24" }]),
  { key: "title", title: "试卷名称" },
  { key: "subject_id", title: "科目", class: "w-24" },
  { key: "grade", title: "年级", class: "w-24" },
  { key: "exam_type", title: "考试类型", class: "w-24" },
  { key: "exam_date", title: "考试日期", class: "w-32" },
  { key: "page_count", title: "页数", class: "w-20" },
]);

/** Status display mapping (storage values stay English/integer). */
const STATUS_META: Record<number, { label: string }> = {
  1: { label: "正常" },
  2: { label: "已删除" },
};

function statusLabel(value: number): string {
  return STATUS_META[value]?.label ?? `#${value}`;
}

function statusDotClass(value: number): string {
  switch (value) {
    case 1:
      return "bg-success";
    case 2:
      return "bg-destructive";
    default:
      return "bg-muted-foreground/40";
  }
}

function subjectName(id: number | undefined): string {
  if (id === undefined || id === null) return "—";
  return subjects.value.find((s) => s.id === id)?.name ?? `#${id}`;
}

function examTypeLabel(type: number | undefined): string {
  if (type === undefined || type === null) return "—";
  return EXAM_TYPE_OPTIONS.find((o) => o.value === type)?.label ?? `#${type}`;
}

/** kid_id from the route query; invalid/missing is null (never NaN). */
function readKidQuery(): number | null {
  const raw = route.query.kid_id;
  const value = Array.isArray(raw) ? raw[0] : raw;
  if (typeof value !== "string") return null;
  const n = Number(value);
  return Number.isInteger(n) && n > 0 ? n : null;
}

/** Load one page with cursor + filters; 0/empty means "all", so that param is not sent. */
async function load(): Promise<void> {
  loading.value = true;
  error.value = "";
  try {
    const cursor = cursorStack.value[cursorStack.value.length - 1] ?? 0;
    const params = {
      limit: PAGE_SIZE,
      ...(cursor > 0 ? { cursor } : {}),
      ...(subjectId.value > 0 ? { subject_id: subjectId.value } : {}),
      ...(grade.value > 0 ? { grade: grade.value } : {}),
      ...(status.value > 0 ? { status: status.value } : {}),
      ...(scopeKidId.value !== null ? { kid_id: scopeKidId.value } : {}),
    };
    data.value = await examApi.list(params);
    hasMore.value = data.value.has_more;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
    data.value = null;
  } finally {
    loading.value = false;
  }
}

/**
 * Reset all filters (including the kid filter back to "all"), then reload page 1
 * with the new conditions.
 */
function applyFilters(): void {
  resetCursor();
  void load();
}

function resetFilters(): void {
  kidFilter.value = 0;
  subjectId.value = 0;
  grade.value = 0;
  status.value = 0;
  applyFilters();
}

/** Back to the first page (clear the cursor stack). */
function resetCursor(): void {
  cursorStack.value = [0];
}

/** Next page: push this page's next_cursor onto the stack. */
function nextPage(): void {
  const next = data.value?.next_cursor ?? 0;
  if (next === 0) return;
  cursorStack.value = [...cursorStack.value, next];
  void load();
}

/** Previous page: pop the stack. */
function prevPage(): void {
  if (cursorStack.value.length <= 1) return;
  cursorStack.value = cursorStack.value.slice(0, -1);
  void load();
}

/**
 * First mount: fetch the student list first (needed by the parent-side kid filter
 * and the "ownership" column), then pull page 1. The kid side only sees its own
 * exams (kid id comes from the session) and does not fetch parent-side /api/kids.
 */
onMounted(async () => {
  if (!isKidRoute.value && kidStore.list.length === 0) await kidStore.fetchList();
  void load();
  try {
    subjects.value = await subjectApi.list();
  } catch {
    // A failed subject-dictionary fetch should not take down the whole page: the
    // filter degrades to "all" and the subject column shows #id.
    subjects.value = [];
  }
});
</script>
