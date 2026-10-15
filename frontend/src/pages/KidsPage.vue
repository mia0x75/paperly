<template>
  <div class="flex flex-col gap-6">
    <h1 class="text-xl font-semibold">学生</h1>

    <section class="bg-card text-card-foreground flex flex-col gap-4 rounded-xl border p-6 shadow-sm">
      <div class="flex items-center justify-between">
        <Button class="ml-auto" icon="person_add" @click="openCreate">新增学生</Button>
      </div>

      <!-- Filtering is server-side (GET /api/kids supports status/grade);
           explicit button rather than query-on-input. -->
      <div class="flex flex-wrap items-center gap-2">
        <!-- grade is INTEGER: use a dropdown. Free text would be silently ignored
             by the backend intParam, and the user would think they filtered when
             they did not. -->
        <Select v-model="grade" :options="gradeOptions" class="w-36" @change="applyFilters" />
        <Select v-model="status" :options="statusOptions" class="w-32" @change="applyFilters" />
        <Button variant="outline" icon="search" @click="applyFilters">筛选</Button>
        <Button variant="ghost" @click="resetFilters">重置</Button>
      </div>

      <ErrorState v-if="error" :message="error" retry-label="重试" @retry="load" />

      <!-- Table sits directly on the section card (same as settings-page lists):
           section is already a solid bg-card. -->
      <Table v-if="loading || items.length > 0" :columns="columns" :data="rows">
        <template #cell-grade="{ value }">
          {{ gradeLabelOr(Number(value)) }}
        </template>
        <template #cell-status="{ value }">
          <StatusBadge :variant="kidStatusVariant(Number(value))">
            {{ kidStatusLabel(Number(value)) }}
          </StatusBadge>
        </template>
        <template #cell-actions="{ row }">
          <Button variant="link" size="sm" @click="openKid(Number(row.id))">详情</Button>
        </template>
      </Table>
      <EmptyState
        v-else-if="!error"
        icon="people"
        title="还没有学生"
        description="先创建一个学生档案，才能录入试卷与成绩。"
        action-label="创建第一个学生"
        @action="openCreate"
      />

      <p class="text-muted-foreground text-sm">
        共 {{ items.length }} 条<template v-if="loading"> · 加载中…</template>
      </p>
    </section>

    <KidCreateDialog ref="createDialog" @created="onCreated" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import Button from "../components/ui/Button.vue";
import EmptyState from "../components/EmptyState.vue";
import ErrorState from "../components/ErrorState.vue";
import KidCreateDialog from "../components/KidCreateDialog.vue";
import StatusBadge from "../components/StatusBadge.vue";
import Table, { type TableColumn } from "../components/Table.vue";
import Select from "../components/ui/Select.vue";
import { kidApi } from "../api/kid";
import { GRADE_OPTIONS, gradeLabelOr } from "../lib/grade";
import { kidStatusLabel, kidStatusVariant } from "../lib/kid";
import { KID_STATUS, type KidSummary } from "../api/types";
import type { SelectOption } from "../lib/ui";

/**
 * KidsPage: the student list (`/kids`).
 *
 * Filtering is server-side; the endpoint returns an unpaginated array (pager would
 * need backend page/page_size). Deletion lives on the detail page; the list only
 * searches and navigates in.
 */
const gradeOptions: readonly SelectOption<number>[] = [
  { value: 0, label: "全部年级" },
  ...GRADE_OPTIONS,
];

const statusOptions: readonly SelectOption<number>[] = [
  { value: 0, label: "全部状态" },
  { value: KID_STATUS.ACTIVE, label: "在读" },
  { value: KID_STATUS.GRADUATED, label: "已毕业" },
  { value: KID_STATUS.DELETED, label: "已删除" },
];

const router = useRouter();

const items = ref<KidSummary[]>([]);
const loading = ref(false);
const error = ref("");
const grade = ref(0);
const status = ref(0);

const rows = computed<Record<string, unknown>[]>(
  () => items.value as unknown as Record<string, unknown>[],
);

const columns: TableColumn[] = [
  { key: "name", title: "姓名" },
  { key: "grade", title: "年级", class: "w-24" },
  { key: "status", title: "状态", class: "w-20" },
  { key: "actions", title: "操作", class: "w-20" },
];

async function load(): Promise<void> {
  loading.value = true;
  error.value = "";
  try {
    const params: { status?: number; grade?: number } = {};
    if (status.value !== 0) params.status = status.value;
    if (grade.value > 0) params.grade = grade.value;
    items.value = await kidApi.list(params);
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
    items.value = [];
  } finally {
    loading.value = false;
  }
}

function applyFilters(): void {
  void load();
}

function resetFilters(): void {
  grade.value = 0;
  status.value = 0;
  void load();
}

function openKid(id: number): void {
  void router.push(`/kids/${id}`);
}

const createDialog = ref<InstanceType<typeof KidCreateDialog> | null>(null);

/** List-page entry point: register via dialog, without navigating to /kids/new. */
function openCreate(): void {
  createDialog.value?.open();
}

async function onCreated(): Promise<void> {
  await load();
}

onMounted(load);
</script>
