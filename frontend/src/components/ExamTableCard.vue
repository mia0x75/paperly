<template>
  <div
    data-testid="exam-table-card"
    class="bg-card text-card-foreground flex flex-col gap-4 rounded-xl border py-4 shadow-sm"
  >
    <div class="flex items-center justify-between gap-3 px-4">
      <div class="flex items-center gap-2 text-base font-medium">
        <Icon name="description" :size="20" />
        {{ kid ? "我的最新试卷" : "最新试卷" }}
      </div>
      <!-- Parent-side card-level filter: defaults to "all", selecting a kid re-fetches
           only this table; the kid side is locked to itself, no filter. -->
      <KidScopeFilter v-if="!kid" v-model="scopeKidId" />
    </div>

    <div v-if="error" class="px-4">
      <ErrorState :message="error" @retry="load" />
    </div>

    <div v-else class="flex flex-col gap-3 px-4">
      <Table :columns="columns" :data="loading ? [] : rows">
        <!-- Ownership: the parent side defaults to all kids and marks per row who the exam belongs to;
              the kid side is locked to their own exams, so a column always showing "self" is pure
              redundancy and is rendered only on the parent side. -->
        <template #cell-kid_id="{ row }">
          {{ kidNameById(kidStore.list, row.kid_id) }}
        </template>
        <!-- Title is the entry: clicking it goes straight to the detail. The home page only does "quick
              glance + quick entry", no longer offering preview/delete -- preview is an empty-shell dialog
              (page images not wired in), and delete is an irreversible dangerous operation crammed into
              the same column as "view"; both are better done in the exam detail page. -->
        <template #cell-title="{ row }">
          <span class="inline-flex items-center gap-2">
            <!-- Status dot before the name: a compact at-a-glance marker (green=normal,
                 red=deleted). Title/aria-label carry the meaning without taking width. -->
            <LegendChip
              bare
              :dot-class="statusDotClass(row.status)"
              :title="statusLabel(row.status)"
              :aria-label="statusLabel(row.status)"
              role="img"
            />
            <router-link
              :to="`${baseHref}/${row.id}`"
              class="hover:text-foreground text-foreground transition-colors no-underline"
              :title="row.title ?? `试卷 #${row.id}`"
            >
              {{ row.title ?? `试卷 #${row.id}` }}
            </router-link>
          </span>
        </template>
        <template #cell-subject_id="{ row }">
          {{ subjectName(row.subject_id) }}
        </template>
        <template #cell-grade="{ row }">
          {{ gradeLabelOr(row.grade) }}
        </template>
        <template #cell-exam_date="{ row }">
          {{ row.exam_date ?? "—" }}
        </template>
        <template #cell-page_count="{ row }">
          <span class="tabular-nums">{{ row.page_count }} 页</span>
        </template>
      </Table>

      <!-- Status-dot legend (same convention as the exam-list page). -->
      <div
        v-if="rows.length > 0"
        class="text-muted-foreground flex items-center justify-center gap-4 text-xs"
      >
        <span class="hover:bg-accent hover:text-accent-foreground inline-flex items-center gap-1 rounded-xl bg-accent/60 px-2.5 py-1 sm:bg-transparent">
          <span class="bg-success inline-block size-2 rounded-full"></span>正常
        </span>
        <span class="hover:bg-accent hover:text-accent-foreground inline-flex items-center gap-1 rounded-xl bg-accent/60 px-2.5 py-1 sm:bg-transparent">
          <span class="bg-destructive inline-block size-2 rounded-full"></span>删除
        </span>
      </div>

      <!-- Home page shows a fixed TOP_N summary; full search lives at /exams.
           No total under cursor pagination — has_more expresses "more", and it stays
           persistent as the only entry to the full list. -->
      <div v-if="rows.length > 0" class="flex items-center justify-end">
        <Button variant="outline" size="sm" icon="arrow_forward" @click="goToList">更多</Button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import Button from "./ui/Button.vue";
import ErrorState from "./ErrorState.vue";
import Icon from "./Icon.vue";
import KidScopeFilter from "./KidScopeFilter.vue";
import LegendChip from "./LegendChip.vue";
import Table, { type TableColumn } from "./Table.vue";
import { gradeLabelOr } from "../lib/grade";
import { kidNameById } from "../lib/kid";
import { examApi } from "../api/exam";
import { subjectApi, type Subject } from "../api/subject";
import { useKidStore } from "../stores/kid";
import type { ExamSummary } from "../api/types";

/**
 * ExamTableCard -- ROW 4 historical exam table.
 *
 * Parent side defaults to all kids (card-top-right switch to one); kid side is locked
 * to itself. Decoupled from the top-bar "current kid" — it no longer sets this scope.
 */
const props = defineProps<{ kidId: number; kid?: boolean }>();

/** Kid-side links go through /kid/exams/*; parent side keeps /exams/*. */
const baseHref = computed(() => (props.kid ? "/kid/exams" : "/exams"));

const TOP_N = 10;

const columns: TableColumn[] = [
  ...(props.kid ? [] : [{ key: "kid_id", title: "归属", class: "w-24" }]),
  { key: "title", title: "试卷名称" },
  { key: "subject_id", title: "科目", class: "w-24" },
  { key: "grade", title: "年级", class: "w-24" },
  { key: "exam_date", title: "考试/归档时间", class: "w-40" },
  { key: "page_count", title: "页数", class: "w-20" },
];

/** Row shape reuses the backend DTO directly. */
type Row = ExamSummary;

const router = useRouter();
const kidStore = useKidStore();

const rows = ref<Row[]>([]);
const subjects = ref<Subject[]>([]);
const hasMore = ref(false);
const error = ref("");
const loading = ref(false);

const scopeKidId = ref<number | null>(props.kid ? props.kidId : null);

function subjectName(id: number | undefined): string {
  if (id === undefined) return "—";
  return subjects.value.find((s) => s.id === id)?.name ?? `#${id}`;
}

/** exams.status (INTEGER, aligned with backend domain.ExamStatus*): 1 active / 2 deleted. */
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

async function load(): Promise<void> {
  loading.value = true;
  error.value = "";
  try {
    const res = await examApi.list({
      limit: TOP_N,
      // undefined is skipped by the request layer (= all kids)
      kid_id: scopeKidId.value ?? undefined,
    });
    rows.value = res.items ?? [];
    hasMore.value = res.has_more;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
    rows.value = [];
    hasMore.value = false;
  } finally {
    loading.value = false;
  }
}

function goToList(): void {
  void router.push(baseHref.value);
}

// Kid side follows the passed kidID; parent side manages its own filter.
watch(
  () => props.kidId,
  (v) => {
    if (props.kid) scopeKidId.value = v;
  },
);

watch(scopeKidId, () => void load());

onMounted(async () => {
  void load();
  try {
    // Fall back to []: a null leak would throw in render and blank the whole table.
    subjects.value = (await subjectApi.list()) ?? [];
  } catch {
    // A failed subject fetch must not take down the table (column falls back to #id).
    subjects.value = [];
  }
});
</script>
