<template>
  <section id="section-tasks" class="bg-card text-card-foreground flex flex-col gap-4 rounded-xl border p-6 shadow-sm">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h2 class="text-base font-semibold tracking-tight">异步与后台队列</h2>
      <!-- Mobile: status + refresh share one equal-width row, "清理完成" takes its
           own full-width row below; desktop: single inline row. DOM order puts the
           full-width button last so col-span-2 lands it on its own row. -->
      <div class="grid w-full grid-cols-2 gap-2 sm:flex sm:w-auto sm:items-center">
        <Select v-model="status" :options="statusOptions" class="w-full sm:w-32" @change="load" />
        <Button variant="outline" class="w-full sm:w-auto" icon="refresh" :loading="loading" @click="load">刷新</Button>
        <Button
          variant="outline"
          class="col-span-2 w-full sm:col-span-1 sm:w-auto"
          icon="delete"
          :loading="clearing"
          :disabled="!canClear"
          @click="clearOpen = true"
        >
          清理完成
        </Button>
      </div>
    </div>

    <ErrorState v-if="error !== ''" :message="error" retry-label="重试" @retry="load" />
    <div v-else-if="loading && tasks.length === 0" class="space-y-2">
      <div v-for="i in 4" :key="i" class="bg-muted animate-pulse h-11 rounded-lg" />
    </div>
    <EmptyState v-else-if="tasks.length === 0" icon="pending" title="暂无任务" description="后台聚合、备份等任务会出现在这里" />

    <ul v-else class="divide-border/60 divide-y">
      <li v-for="t in tasks" :key="t.id" class="flex flex-wrap items-center gap-3 py-2.5 text-sm">
        <span class="text-muted-foreground font-mono text-sm">#{{ t.id }}</span>
        <!-- Status is a dot (not a text badge): matches the home/exam-list status
             convention and keeps the row compact. title/aria-label carry the label. -->
        <LegendChip
          bare
          :dot-class="statusDotClass(t.status)"
          :title="statusLabel(t.status)"
          :aria-label="statusLabel(t.status)"
          role="img"
        />
        <span class="font-medium">{{ kindLabel(t.kind) }}</span>
        <span class="text-muted-foreground text-sm font-normal tabular-nums">{{ t.created_at }}</span>
        <span v-if="t.retry_count > 0" class="text-muted-foreground text-sm font-normal">
          重试 {{ t.retry_count }} 次
        </span>
        <span class="ml-auto flex items-center gap-2">
          <Button variant="ghost" size="sm" icon="description" @click="openDetail(t.id)">日志</Button>
          <Button
            v-if="t.status === TASK_STATUS.failed"
            variant="ghost"
            size="sm"
            icon="refresh"
            @click="retry(t.id)"
          >
            重试
          </Button>
        </span>
      </li>
    </ul>

    <!-- Status-dot legend: one dot per task status so the list's colors are decodable.
         Mobile: 6 statuses in a 3-column grid (2 rows, left-center-right aligned) with a
         persistent accent chip; chips size to their content and center in the cell
         (justify-self-center — grid items stretch by default and adjacent chips would
         touch edge to edge), plus a horizontal column gap for breathing room. Desktop:
         single centered row, chips transparent until hover (unchanged). -->
    <div
      v-if="tasks.length > 0"
      class="text-muted-foreground grid grid-cols-3 gap-x-2 gap-y-2.5 text-xs sm:flex sm:flex-wrap sm:items-center sm:justify-center sm:gap-x-4 sm:gap-y-1"
    >
      <LegendChip
        v-for="s in LEGEND_STATUSES"
        :key="s"
        class="justify-center justify-self-center"
        :dot-class="statusDotClass(s)"
        >{{ statusLabel(s) }}</LegendChip
      >
    </div>

    <Button v-if="hasMore" variant="ghost" size="sm" class="self-center" @click="loadMore">
      加载更多
    </Button>

    <Modal :show="detailOpen" :title="detail ? `任务 #${detail.id} 详情` : '任务详情'" @close="detailOpen = false">
      <div v-if="detail" class="text-sm">
        <!-- Header: task type + status share one row; the status dot reuses the list's color. -->
        <div class="border-border/50 flex flex-wrap items-center justify-between gap-2 border-b pb-3">
          <span><span class="text-muted-foreground">任务类型：</span>{{ kindLabel(detail.kind) }}</span>
          <span class="flex items-center gap-2">
            <span class="text-muted-foreground">任务状态：</span>
            <span class="inline-block size-2 rounded-full" :class="statusDotClass(detail.status)" aria-hidden="true" />
            <span>{{ statusLabel(detail.status) }}</span>
          </span>
        </div>

        <!-- Times: created / started / finished, with elapsed appended to finished. -->
        <div class="border-border/50 mt-3 space-y-1.5 border-b pb-3">
          <p class="m-0 flex gap-3 py-1">
            <span class="text-muted-foreground w-20 shrink-0">创建时间：</span>
            <span class="tabular-nums">{{ detail.created_at || "—" }}</span>
          </p>
          <p class="m-0 flex gap-3 py-1">
            <span class="text-muted-foreground w-20 shrink-0">开始时间：</span>
            <span class="tabular-nums">{{ detail.started_at || "—" }}</span>
          </p>
          <p class="m-0 flex gap-3 py-1">
            <span class="text-muted-foreground w-20 shrink-0">完成时间：</span>
            <span class="tabular-nums">
              {{ detail.finished_at || "—" }}<template v-if="durationText !== ''">（{{ durationText }}）</template>
            </span>
          </p>
        </div>

        <!-- Close: mobile full width, desktop right-aligned (this replaces the removed global X). -->
        <div class="mt-3 flex sm:justify-end">
          <Button variant="outline" class="w-full sm:w-24" @click="detailOpen = false">关闭</Button>
        </div>

        <!-- Error: only on failure. Collapsed by default; the expanded log is height-capped + scrollable. -->
        <div v-if="hasError" class="border-border/50 mt-3 border-t pt-3">
          <div class="flex items-center justify-between gap-2">
            <button
              type="button"
              class="hover:bg-accent hover:text-accent-foreground flex items-center gap-1 rounded-md px-1 py-0.5 transition-colors"
              :aria-expanded="errorOpen"
              @click="errorOpen = !errorOpen"
            >
              <Icon :name="errorOpen ? 'expand_more' : 'chevron_right'" :size="16" />
              <span>错误</span>
            </button>
            <Button
              size="sm"
              variant="outline"
              class="w-1/4 shrink-0 sm:w-24"
              :disabled="detail.last_error === ''"
              @click="copyError"
            >
              复制
            </Button>
          </div>
          <pre
            v-if="errorOpen"
            class="bg-muted/40 mt-2 max-h-48 overflow-x-auto overflow-y-auto rounded-lg border p-3 font-mono text-xs leading-relaxed break-all whitespace-pre-wrap"
          >{{ detail.last_error || "—" }}</pre>
        </div>

        <!-- Payload: collapsed by default; expanded log is height-capped + scrollable. -->
        <div class="border-border/50 mt-3 border-t pt-3">
          <div class="flex items-center justify-between gap-2">
            <button
              type="button"
              class="hover:bg-accent hover:text-accent-foreground flex items-center gap-1 rounded-md px-1 py-0.5 transition-colors"
              :aria-expanded="payloadOpen"
              @click="payloadOpen = !payloadOpen"
            >
              <Icon :name="payloadOpen ? 'expand_more' : 'chevron_right'" :size="16" />
              <span>载荷</span>
            </button>
            <Button
              size="sm"
              variant="outline"
              class="w-1/4 shrink-0 sm:w-24"
              :disabled="prettyPayload === '—'"
              @click="copyPayload"
            >
              复制
            </Button>
          </div>
          <pre
            v-if="payloadOpen"
            class="bg-muted/40 mt-2 max-h-64 overflow-x-auto overflow-y-auto rounded-lg border p-3 font-mono text-xs leading-relaxed break-all whitespace-pre-wrap"
          >{{ prettyPayload }}</pre>
        </div>

        <p v-if="copiedNote !== ''" class="text-success mt-2 text-xs">{{ copiedNote }}</p>
      </div>
    </Modal>

    <ConfirmDialog
      :show="clearOpen"
      title="清理已完成任务"
      message="将删除所有已完成/已跳过/已取消的任务记录，进行中的任务不受影响。"
      confirm-label="清理"
      :loading="clearing"
      @cancel="clearOpen = false"
      @confirm="clearFinished"
    />
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import Button from "../ui/Button.vue";
import ConfirmDialog from "../ConfirmDialog.vue";
import EmptyState from "../EmptyState.vue";
import ErrorState from "../ErrorState.vue";
import Icon from "../Icon.vue";
import LegendChip from "../LegendChip.vue";
import Modal from "../Modal.vue";
import Select from "../ui/Select.vue";
import { settingsApi } from "../../api/settings";
import { TASK_KIND_TEXT, TASK_STATUS, TASK_STATUS_TEXT } from "../../api/types";
import type { TaskDetail, TaskSummary } from "../../api/types";
import { taskDurationText } from "../../lib/task";
import type { SelectOption } from "../../lib/ui";

/**
 * TasksSection — the "async & background queue" section of the settings page (tasks table).
 *
 * The status filter's "all" omits the param: backend status=0 is a legal pending,
 * so it can't serve as the "don't filter" sentinel.
 */
const tasks = ref<TaskSummary[]>([]);
const ALL_STATUS = -1;
const status = ref(ALL_STATUS);
const loading = ref(false);
const clearing = ref(false);
const error = ref("");
const nextCursor = ref(0);
const hasMore = ref(false);

const detailOpen = ref(false);
const detail = ref<TaskDetail | null>(null);
const clearOpen = ref(false);
const errorOpen = ref(false);
const payloadOpen = ref(false);
const copiedNote = ref("");

const statusOptions: readonly SelectOption<number>[] = [
  { value: ALL_STATUS, label: "全部状态" },
  { value: TASK_STATUS.pending, label: "待执行" },
  { value: TASK_STATUS.running, label: "执行中" },
  { value: TASK_STATUS.done, label: "已完成" },
  { value: TASK_STATUS.failed, label: "失败" },
  { value: TASK_STATUS.skipped, label: "已跳过" },
  { value: TASK_STATUS.cancelled, label: "已取消" },
];

/**
 * Task payload pretty-print: the backend stores compact one-line JSON (unreadable on
 * mobile), so re-indent here; non-JSON falls back to the raw string.
 */
const prettyPayload = computed<string>(() => {
  const raw = detail.value?.detail ?? "";
  if (raw === "") return "—";
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
});

const canClear = computed<boolean>(() => tasks.value.length > 0);

const hasError = computed<boolean>(
  () => detail.value !== null && (detail.value.status === TASK_STATUS.failed || detail.value.last_error !== ""),
);

const durationText = computed<string>(() =>
  taskDurationText(detail.value?.started_at ?? "", detail.value?.finished_at ?? ""),
);

let copiedTimer: ReturnType<typeof setTimeout> | null = null;

async function copyText(text: string): Promise<void> {
  if (text === "" || text === "—") return;
  try {
    await navigator.clipboard.writeText(text);
    copiedNote.value = "已复制到剪贴板";
    if (copiedTimer !== null) clearTimeout(copiedTimer);
    copiedTimer = window.setTimeout(() => {
      copiedNote.value = "";
      copiedTimer = null;
    }, 1500);
  } catch {
    // Clipboard permission denied / unavailable: the text is visible and selectable, so stay silent.
  }
}

onUnmounted(() => {
  // A pending "copied" hint must not fire after unmount (nor keep the timer alive).
  if (copiedTimer !== null) {
    clearTimeout(copiedTimer);
    copiedTimer = null;
  }
});

function copyError(): void {
  void copyText(detail.value?.last_error ?? "");
}

function copyPayload(): void {
  void copyText(prettyPayload.value);
}

function statusLabel(s: number): string {
  return TASK_STATUS_TEXT[s] ?? `状态 ${s}`;
}

function kindLabel(k: number): string {
  return TASK_KIND_TEXT[k] ?? `任务 ${k}`;
}

/**
 * Task status -> dot color, via hue-stable `status-*` tokens (same hue light and dark,
 * so a learned color keeps its meaning across themes).
 */
function statusDotClass(s: number): string {
  switch (s) {
    case TASK_STATUS.done:
      return "bg-status-done";
    case TASK_STATUS.running:
      return "bg-status-running";
    case TASK_STATUS.failed:
      return "bg-status-failed";
    case TASK_STATUS.pending:
      return "bg-status-pending";
    case TASK_STATUS.skipped:
      return "bg-status-skipped";
    case TASK_STATUS.cancelled:
      return "bg-status-cancelled";
    default:
      return "bg-muted-foreground/40";
  }
}

/** Every task status, in lifecycle order. */
const LEGEND_STATUSES: readonly number[] = [
  TASK_STATUS.pending,
  TASK_STATUS.running,
  TASK_STATUS.done,
  TASK_STATUS.failed,
  TASK_STATUS.skipped,
  TASK_STATUS.cancelled,
];

function queryStatus(): number | undefined {
  return status.value === ALL_STATUS ? undefined : status.value;
}

async function load(): Promise<void> {
  loading.value = true;
  error.value = "";
  try {
    const res = await settingsApi.tasks({ status: queryStatus(), cursor: 0, limit: 50 });
    tasks.value = res.items;
    nextCursor.value = res.next_cursor;
    hasMore.value = res.has_more;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    loading.value = false;
  }
}

async function loadMore(): Promise<void> {
  loading.value = true;
  try {
    const res = await settingsApi.tasks({ status: queryStatus(), cursor: nextCursor.value, limit: 50 });
    tasks.value = [...tasks.value, ...res.items];
    nextCursor.value = res.next_cursor;
    hasMore.value = res.has_more;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    loading.value = false;
  }
}

async function retry(id: number): Promise<void> {
  try {
    await settingsApi.retryTask(id);
    await load();
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  }
}

async function openDetail(id: number): Promise<void> {
  try {
    detail.value = await settingsApi.task(id);
    // Reset per-open UI state (no inherited expansions or stale copy hint).
    errorOpen.value = false;
    payloadOpen.value = false;
    copiedNote.value = "";
    detailOpen.value = true;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  }
}

async function clearFinished(): Promise<void> {
  clearing.value = true;
  try {
    await settingsApi.clearFinishedTasks();
    clearOpen.value = false;
    await load();
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    clearing.value = false;
  }
}

onMounted(load);
</script>
