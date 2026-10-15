<template>
  <div class="flex flex-col gap-6">
    <ErrorState v-if="error" :message="error" retry-label="重试" @retry="load" />

    <!-- Layout: title row + button row + "gallery + info card" share one 2fr+auto grid;
         the auto column spans the two buttons (no magic pixels, no JS measuring).
         Mobile grid-cols-1: button row follows the title row, above the gallery. -->
    <!-- v-if includes error: an error page still needs "back to list". -->
    <div v-if="exam || error" class="grid grid-cols-1 gap-6 lg:grid-cols-[2fr_auto]">
      <!-- ═══ Row1 left: back + ownership pill + title ═══ -->
      <div class="flex flex-wrap items-center gap-2">
        <Button variant="outline" icon="arrow_back" @click="router.push(listHref)">返回列表</Button>
        <!-- Ownership pill -->
        <span v-if="isKid" class="bg-muted text-foreground rounded-full px-3 py-2 text-sm font-medium">
          我的试卷：
        </span>
        <span
          v-else-if="examKidName"
          class="bg-muted text-foreground rounded-full px-3 py-2 text-sm font-medium"
        >
          {{ examKidName }}
        </span>
        <h1 class="text-xl font-semibold m-0">{{ exam?.title ?? "试卷详情" }}</h1>
        <StatusBadge v-if="exam" :variant="statusVariant(exam.status)" class="ml-2">
          {{ statusLabel(exam.status) }}
        </StatusBadge>
        <!-- "Saved" stays in the top row: a toast inside the dialog would close with it -->
        <span v-if="saved" class="text-success ml-auto text-sm">已保存</span>
      </div>

      <!-- ═══ Row1 right: actions (same column/width as the info card) ═══ -->
      <div class="flex items-center justify-end gap-2 lg:justify-between">
        <Button variant="outline" icon="edit" :disabled="!exam" @click="openEdit">编辑</Button>
        <!-- Deleted state: "restore" replaces "delete" (mutually exclusive). -->
        <Button
          v-if="exam?.status === EXAM_STATUS.DELETED"
          variant="outline"
          icon="refresh"
          :disabled="restoring"
          @click="confirmRestore = true"
        >
          恢复试卷
        </Button>
        <Button
          v-else
          variant="destructive"
          icon="delete"
          :disabled="!exam"
          @click="confirmDelete = true"
        >
          删除试卷
        </Button>
      </div>

      <!-- ═══ Left column: gallery + summary + editor ═══ -->
      <div class="flex flex-col gap-6">
      <!-- ═══ Row2 left: exam gallery ═══ -->
      <section class="bg-card flex flex-col gap-4 rounded-xl border p-6 shadow-sm">
        <!-- Page-ops status bar: the editor unmounts on dialog close, so hints must live
             in the gallery area to stay visible. -->
        <p v-if="pagesOpsPending" class="text-muted-foreground text-xs">
          页整理处理中，完成后本区自动刷新…
        </p>
        <p v-if="pagesOpsMsg !== ''" class="text-xs" :class="pagesOpsOk ? 'text-success' : 'text-destructive'">
          {{ pagesOpsMsg }}
        </p>

        <!-- Arranging: gallery swaps for the inline editor (operates on these thumbnails). -->
        <ExamPagesEditor
          v-if="pagesEditing"
          :pages="pages"
          :image-urls="pageURLs"
          :saving="pagesOpsSaving"
          @close="pagesEditing = false"
          @save="savePagesOps"
        />

        <template v-else>
          <div class="flex items-center justify-between">
            <Button variant="outline" size="sm" :disabled="pageIndex <= 0" @click="pageIndex--">
              <Icon name="chevron_left" :size="16" />上一页
            </Button>
            <div class="flex items-center gap-3">
              <span class="text-muted-foreground text-sm tabular-nums">
                第 {{ pages.length === 0 ? 0 : pageIndex + 1 }} / {{ pages.length }} 页
              </span>
              <Button
                variant="ghost"
                size="sm"
                icon="search"
                class="hidden sm:inline-flex"
                :disabled="pages.length === 0"
                @click="openZoom"
              >
                放大
              </Button>

            </div>
            <Button
              variant="outline"
              size="sm"
              :disabled="pageIndex >= pages.length - 1"
              @click="pageIndex++"
            >
              下一页<Icon name="chevron_right" :size="16" />
            </Button>
          </div>

          <button
            type="button"
            class="hover:border-foreground/40 group bg-muted flex min-h-80 w-full items-center justify-center overflow-hidden rounded-lg border transition-colors"
            :disabled="pages.length === 0"
            title="点击放大查看这一页"
            @click="openZoom"
          >
            <img
              v-if="currentImageURL !== ''"
              :src="currentImageURL"
              :alt="`第 ${currentPage?.page_index ?? 1} 页`"
              class="max-h-[70vh] max-w-full object-contain"
            />
            <!-- Page image via fetch-blob-URL (see ensurePageImage); loading placeholder doesn't collapse. -->
            <span v-else-if="currentPage" class="text-muted-foreground animate-pulse text-sm">加载中…</span>
            <span v-else class="text-muted-foreground text-sm">无页面</span>
          </button>

          <!-- Thumbnails + "edit exam pages" button share a bottom-aligned row. -->
          <div class="flex flex-wrap items-end gap-2">
            <div v-if="pages.length > 1" class="flex flex-wrap gap-2">
              <button
                v-for="(p, i) in pages"
                :key="p.id"
                type="button"
                class="h-16 w-12 overflow-hidden rounded border transition-all"
                :class="i === pageIndex ? 'border-primary ring-2 ring-primary/30' : 'hover:border-foreground/40'"
                @click="pageIndex = i"
              >
                <img
                  v-if="pageURLs.get(p.id)"
                  :src="pageURLs.get(p.id)"
                  :alt="`P${i + 1}`"
                  class="h-full w-full object-cover"
                />
                <div v-else class="bg-muted h-full w-full animate-pulse" />
              </button>
            </div>
            <Button
              variant="outline"
              size="sm"
              class="ml-auto"
              :disabled="pagesOpsPending || !exam"
              @click="pagesEditing = true"
            >
              编辑试卷页
            </Button>
          </div>
        </template>
      </section>

      <!-- Summary below the gallery: full left-column width for the editor. -->
      <section class="bg-card rounded-xl border p-6 shadow-sm">
        <h2 class="text-base font-semibold tracking-tight">试卷总结</h2>
        <p class="text-muted-foreground mt-1 text-sm">分析试卷，总结经验和教训。</p>
        <div class="mt-4">
          <SummaryView
            :summary="exam?.summary ?? ''"
            :is-kid="isKid"
            :self-kid-id="isKid ? (exam?.kid_id ?? null) : null"
            :deleting-sig="summaryDeletingSig"
            @delete="deleteSummary"
          />
        </div>
        <div class="mt-4 border-t border-border/50 pt-4">
          <SummaryEditor
            ref="summaryEditor"
            :submitting="summarySubmitting"
            @submit="appendSummary"
          />
        </div>
      </section>
      </div>
      <div class="flex flex-col gap-6 lg:col-start-2">
        <section class="bg-card rounded-xl border p-6 shadow-sm">
          <h2 class="text-base font-semibold tracking-tight">试卷基本信息</h2>
          <dl class="mt-4 flex flex-col gap-3 text-sm">
            <div v-for="row in infoRows" :key="row.label">
              <dt class="text-muted-foreground text-xs">{{ row.label }}</dt>
              <dd class="m-0">{{ row.value }}</dd>
            </div>
          </dl>
        </section>
      </div>
    </div>

    <p v-else-if="loading" class="text-muted-foreground text-sm">加载中…</p>

    <div v-if="kidMismatch" class="bg-card text-muted-foreground flex items-center justify-between rounded-xl border p-3 text-sm shadow-sm">
      当前浏览的是 {{ selectedKidName }}
      <Button size="sm" variant="ghost" @click="kidStore.select(exam?.kid_id ?? null)">切换回去</Button>
    </div>


    <!-- Edit in a dialog, not inline in the right column — inline would stretch the
         column and crowd image vs fields; closing returns to pure view. -->
    <Modal :show="showEdit" title="编辑试卷" @close="closeEdit">
      <div class="flex flex-col gap-4">
        <label class="flex flex-col gap-1 text-sm">
          <span class="text-muted-foreground">试卷名称 <span class="text-destructive">*</span></span>
          <input v-model="form.title" type="text" :class="[FIELD_BASE, 'h-9']" />
        </label>
        <!-- Owning kid, parent side only: full-width row; kid side can't reassign (backend force-keeps). -->
        <label v-if="!isKid" class="flex flex-col gap-1 text-sm">
          <span class="text-muted-foreground">归属孩子</span>
          <Select v-model="form.kidId" :options="kidOptions" placeholder="未归属" />
        </label>
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <label class="flex flex-col gap-1 text-sm">
            <span class="text-muted-foreground">科目 <span class="text-destructive">*</span></span>
            <Select v-model="form.subjectId" :options="subjectOptions" placeholder="请选择科目" />
          </label>
          <label class="flex flex-col gap-1 text-sm">
            <span class="text-muted-foreground">年级 <span class="text-destructive">*</span></span>
            <Select v-model="form.grade" :options="gradeOptions" placeholder="请选择年级" />
          </label>
          <label class="flex flex-col gap-1 text-sm">
            <span class="text-muted-foreground">类型 <span class="text-destructive">*</span></span>
            <Select v-model="form.examType" :options="examTypeOptions" placeholder="请选择类型" />
          </label>
          <label class="flex flex-col gap-1 text-sm">
            <span class="text-muted-foreground">考试日期</span>
            <DatePicker v-model="form.examDate" clearable />
          </label>
        </div>
      </div>
      <template #footer>
        <Button variant="ghost" @click="closeEdit">取消</Button>
        <Button icon="save" :loading="saving" :disabled="!canSave" @click="save">保存</Button>
      </template>
    </Modal>

    <!-- Zoom view in ExamZoomOverlay (separate: wheel/drag/fit-axis would bloat this page;
         see the three trade-offs in that file's header). -->
    <ExamZoomOverlay
      :show="zoomOpen"
      :title="exam?.title ?? '试卷详情'"
      :image-src="currentImageURL"
      :alt="currentPage === null ? '' : `第 ${currentPage.page_index} 页`"
      :page-index="pageIndex"
      :page-count="pages.length"
      @close="closeZoom"
      @prev="pageIndex--"
      @next="pageIndex++"
    />

    <ConfirmDialog
      :show="confirmDelete"
      :title="`确认删除「${exam?.title ?? ''}」？`"
      message="删除后不再纳入试卷统计，磁盘文件保留，可随时恢复。"
      :loading="deleting"
      @confirm="remove"
      @cancel="confirmDelete = false"
    />

    <ConfirmDialog
      :show="confirmRestore"
      :title="`确认恢复「${exam?.title ?? ''}」？`"
      message="恢复后重新纳入试卷统计。"
      confirm-label="确认恢复"
      :loading="restoring"
      @confirm="restore"
      @cancel="confirmRestore = false"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import Button from "../components/ui/Button.vue";
import { FIELD_BASE } from "../lib/ui";
import ConfirmDialog from "../components/ConfirmDialog.vue";
import ErrorState from "../components/ErrorState.vue";
import ExamPagesEditor from "../components/ExamPagesEditor.vue";
import ExamZoomOverlay from "../components/ExamZoomOverlay.vue";
import Icon from "../components/Icon.vue";
import Modal from "../components/Modal.vue";
import StatusBadge from "../components/StatusBadge.vue";
import SummaryEditor from "../components/SummaryEditor.vue";
import SummaryView from "../components/SummaryView.vue";
import Select from "../components/ui/Select.vue";
import DatePicker from "../components/ui/DatePicker.vue";
import { examApi } from "../api/exam";
import { fetchBlob } from "../api/client";
import { settingsApi } from "../api/settings";
import { subjectApi, type Subject } from "../api/subject";
import { useKidStore } from "../stores/kid";
import { formatDateTime } from "../lib/date";
import { GRADE_OPTIONS, gradeLabelOr } from "../lib/grade";
import { kidNameById } from "../lib/kid";
import { EXAM_TYPE_OPTIONS } from "../lib/examTitle";
import { TASK_STATUS, KID_STATUS, EXAM_STATUS, type ExamDetail, type PageOpsPayload, type PageSummary } from "../api/types";
import type { SelectOption } from "../lib/ui";

/**
 * ExamViewerPage: exam view/edit (`/exams/:id`).
 *
 * Left is the page-image gallery, right read-only info; edit/delete go through the
 * top-bar buttons. Editing submits all editable fields: the backend PUT is a full-row
 * overwrite where nil clears, so the form initializes from detail and submits everything.
 */
const route = useRoute();
const router = useRouter();
/** Map kid_id to a name via the top bar's global list (exams stores only the FK). */
const kidStore = useKidStore();

const isKid = computed(() => route.meta.audience === "kid");

/** Kid side returns to /kid/exams; parent side stays at /exams. */
const listHref = computed(() => (isKid.value ? "/kid/exams" : "/exams"));
const examKidName = computed(() => {
  if (!exam.value?.kid_id) return '';
  // Kid side never reads /api/kids (requireParent would kick to login); its exam is just "me".
  if (isKid.value) return '我';
  const k = kidStore.list.find((x) => x.id === exam.value!.kid_id);
  return k?.name ?? String(exam.value.kid_id);
});
const selectedKidName = computed(() => {
  if (!kidStore.selectedId) return '';
  const k = kidStore.list.find((x) => x.id === kidStore.selectedId);
  return k?.name ?? String(kidStore.selectedId);
});
const kidMismatch = computed(() =>
  !isKid.value
  && exam.value !== null
  && exam.value.kid_id !== null
  && exam.value.kid_id !== kidStore.selectedId,
);
const id = Number(route.params.id);

const exam = ref<ExamDetail | null>(null);
const subjects = ref<Subject[]>([]);
const loading = ref(false);
const error = ref("");
const saving = ref(false);
const saved = ref(false);
const deleting = ref(false);
const restoring = ref(false);
const confirmDelete = ref(false);
const confirmRestore = ref(false);
const showEdit = ref(false);
const pageIndex = ref(0);
const zoomOpen = ref(false);

/** Summary append/delete in-flight state. */
const summarySubmitting = ref(false);
const summaryDeletingSig = ref("");
/** Summary editor handle (cleared after append). */
const summaryEditor = ref<{ clear: () => void } | null>(null);

// ---------- Page-level ops (delete / add / drag-reorder -> one reorder_pages task) ----------
/** Editor expanded inline (gallery swaps for ExamPagesEditor). */
const pagesEditing = ref(false);
/** Ops request in flight (pre-202; editor save shows loading). */
const pagesOpsSaving = ref(false);
/** Polling window post-202 enqueue (arranging disabled meanwhile). */
const pagesOpsPending = ref(false);
const pagesOpsMsg = ref("");
const pagesOpsOk = ref(false);
/** Poll generation: bumping invalidates the old run (exits in place on cancel/unmount). */
let pagesOpsSeq = 0;
let pagesOpsTaskId = 0;
let pagesOpsTimer: ReturnType<typeof setTimeout> | null = null;

const subjectOptions = computed<readonly SelectOption<number>[]>(() =>
  subjects.value.map((s) => ({ value: s.id, label: s.name })),
);
const gradeOptions: readonly SelectOption<number>[] = GRADE_OPTIONS;
const examTypeOptions: readonly SelectOption<number>[] = EXAM_TYPE_OPTIONS;

/**
 * Owning-kid options: active kids, plus the exam's current kid even if inactive —
 * else a graduated owner's select shows blank while a no-touch save keeps the id.
 */
const kidOptions = computed<readonly SelectOption<number>[]>(() => {
  const opts = kidStore.list
    .filter((k) => k.status === KID_STATUS.ACTIVE)
    .map((k) => ({ value: k.id, label: k.name }));
  const cur = exam.value?.kid_id ?? 0;
  if (cur > 0 && !opts.some((o) => o.value === cur)) {
    opts.push({ value: cur, label: kidNameById(kidStore.list, cur) });
  }
  return opts;
});

const form = reactive({
  title: "",
  kidId: 0,
  subjectId: 0,
  grade: 0,
  examType: 0,
  examDate: "",
});

/** Title required; subject/grade/type must be concrete (never "unset"). */
const canSave = computed(
  () => form.title.trim() !== "" && form.subjectId > 0 && form.grade > 0 && form.examType > 0,
);

const pages = computed(() => exam.value?.pages ?? []);
const currentPage = computed(() => pages.value[pageIndex.value] ?? null);

/**
 * Page-image loading: `/api/pages/:id/file` sits behind the requireAny gate, but
 * `<img :src>` can't carry Authorization — direct use 401s everything. Fetch bytes
 * with fetchBlob (credentials in header), convert to a blob URL for <img>.
 *
 * URLs cache per page within this instance, revoked wholesale on unmount (no sharing,
 * no invalidation problem); re-entering refetches.
 */
const pageURLs = reactive(new Map<number, string>());
/** Unmount flag: don't create a URL after destroy (created-but-unrevoked leaks). */
let disposed = false;

/** Bounded image retry: 15 x 3s, covering file moves during a reorder task. */
const IMAGE_RETRY_MS = 3000;
const IMAGE_RETRY_MAX = 15;
const imageAttempts = new Map<number, number>();
const imageTimers = new Set<ReturnType<typeof setTimeout>>();

function ensurePageImage(pageId: number): void {
  // Optimistic placeholder pages (negative id) have no file; skip, or doomed 404s spin 15 retry rounds.
  if (pageId <= 0) return;
  if (pageURLs.has(pageId)) return;
  fetchBlob(`/pages/${pageId}/file`)
    .then((blob) => {
      if (disposed) return;
      imageAttempts.delete(pageId);
      pageURLs.set(pageId, URL.createObjectURL(blob));
    })
    .catch(() => {
      // One page's failure never Errors the page: it stays loading, retries boundedly, then placeholders; re-entry resets.
      if (disposed) return;
      const n = (imageAttempts.get(pageId) ?? 0) + 1;
      imageAttempts.set(pageId, n);
      if (n > IMAGE_RETRY_MAX) return;
      const timer = setTimeout(() => {
        imageTimers.delete(timer);
        if (!disposed) ensurePageImage(pageId);
      }, IMAGE_RETRY_MS);
      imageTimers.add(timer);
    });
}

/** Blob URL for the current page; "" when not ready (template shows a placeholder). */
const currentImageURL = computed(() => {
  const p = currentPage.value;
  return p === null ? "" : (pageURLs.get(p.id) ?? "");
});

watch(
  pages,
  (list) => {
    for (const p of list) ensurePageImage(p.id);
  },
  { immediate: true },
);

onUnmounted(() => {
  disposed = true;
  // Clear polling + retry timers on unmount (late callbacks exit in place, but the objects must not outlive).
  pagesOpsSeq += 1;
  if (pagesOpsTimer !== null) {
    clearTimeout(pagesOpsTimer);
    pagesOpsTimer = null;
  }
  for (const timer of imageTimers) clearTimeout(timer);
  imageTimers.clear();
  imageAttempts.clear();
  for (const url of pageURLs.values()) URL.revokeObjectURL(url);
  pageURLs.clear();
});

const STATUS_META: Record<number, { label: string; variant: "success" | "destructive" }> = {
  [EXAM_STATUS.ACTIVE]: { label: "正常", variant: "success" },
  [EXAM_STATUS.DELETED]: { label: "已删除", variant: "destructive" },
};

function statusLabel(s: number): string {
  return STATUS_META[s]?.label ?? `#${s}`;
}

function statusVariant(s: number): "success" | "destructive" | "secondary" {
  return STATUS_META[s]?.variant ?? "secondary";
}

const infoRows = computed(() => {
  const p = exam.value;
  if (p === null) return [];
  const subject = subjects.value.find((s) => s.id === p.subject_id)?.name ?? "—";
  const type = EXAM_TYPE_OPTIONS.find((o) => o.value === p.exam_type)?.label ?? "—";
  return [
    { label: "归属孩子", value: isKid.value ? "我" : kidNameById(kidStore.list, p.kid_id) },
    { label: "科目", value: subject },
    { label: "年级", value: gradeLabelOr(p.grade) },
    { label: "考试类型", value: type },
    { label: "考试日期", value: p.exam_date ?? "—" },
    { label: "录入时间", value: formatDateTime(p.created_at) || "—" },
    { label: "文件体量", value: `${p.page_count} 张图片` },
  ];
});

async function load(): Promise<void> {
  if (!Number.isFinite(id) || id <= 0) {
    error.value = "无效的试卷 ID";
    return;
  }
  loading.value = true;
  error.value = "";
  try {
    const [p, subs] = await Promise.all([examApi.get(id), subjectApi.list()]);
    exam.value = p;
    subjects.value = subs;
    pageIndex.value = 0;
    form.title = p.title ?? "";
    form.kidId = p.kid_id ?? 0;
    form.subjectId = p.subject_id ?? 0;
    form.grade = p.grade ?? 0;
    form.examType = p.exam_type ?? 0;
    form.examDate = p.exam_date ?? "";
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    loading.value = false;
  }
}

/**
 * openEdit resets the form from the exam every time: dirty values from a cancelled
 * edit must not revive (reactive singleton).
 */
function openEdit(): void {
  const p = exam.value;
  if (p === null) return;
  form.title = p.title ?? "";
  form.kidId = p.kid_id ?? 0;
  form.subjectId = p.subject_id ?? 0;
  form.grade = p.grade ?? 0;
  form.examType = p.exam_type ?? 0;
  form.examDate = p.exam_date ?? "";
  saved.value = false;
  showEdit.value = true;
}

function closeEdit(): void {
  showEdit.value = false;
}

/** openZoom only toggles; factor/fit/wheel/drag live in ExamZoomOverlay. */
function openZoom(): void {
  if (pages.value.length === 0) return;
  zoomOpen.value = true;
}

function closeZoom(): void {
  zoomOpen.value = false;
}

async function save(): Promise<void> {
  if (!canSave.value) return;
  saving.value = true;
  saved.value = false;
  error.value = "";
  try {
    // Kid side passes current ownership through (backend force-keeps); parent: 0 = unassigned, else picked.
    const updated = await examApi.update(id, {
      title: form.title.trim(),
      subject_id: form.subjectId,
      grade: form.grade,
      exam_type: form.examType,
      exam_date: form.examDate || null,
      kid_id: isKid.value ? (exam.value?.kid_id ?? null) : (form.kidId > 0 ? form.kidId : null),
      summary: exam.value?.summary ?? null,
    });
    // Update response omits pages (detail without gallery); editing never touches them —
    // merge the local gallery (+ derived page_count) instead of re-GET.
    exam.value = {
      ...exam.value,
      ...updated,
      page_count: exam.value?.pages.length ?? updated.page_count,
      pages: exam.value?.pages ?? updated.pages,
    };
    saved.value = true;
    showEdit.value = false;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    saving.value = false;
  }
}

/**
 * Append a signed summary segment. Backend returns the full summary: patch in place
 * (no GET); clear the editor only on success (failed append keeps the draft).
 */
async function appendSummary(html: string): Promise<void> {
  summarySubmitting.value = true;
  error.value = "";
  try {
    const res = await examApi.appendSummary(id, html);
    if (exam.value) exam.value = { ...exam.value, summary: res.summary };
    summaryEditor.value?.clear();
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    summarySubmitting.value = false;
  }
}

/** Delete one signed summary segment (parent any; kid own only). */
async function deleteSummary(sig: string): Promise<void> {
  summaryDeletingSig.value = sig;
  error.value = "";
  try {
    const res = await examApi.deleteSummary(id, sig);
    if (exam.value) exam.value = { ...exam.value, summary: res.summary };
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    summaryDeletingSig.value = "";
  }
}

/**
 * Soft-delete: refresh detail in place (unfiltered Get still returns it); badge/button
 * follow exam.status.
 */
async function remove(): Promise<void> {
  deleting.value = true;
  error.value = "";
  try {
    await examApi.remove(id);
    exam.value = await examApi.get(id);
    confirmDelete.value = false;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    deleting.value = false;
  }
}

/**
 * Restore: refresh in place; badge/button follow exam.status, no manual switching.
 */
async function restore(): Promise<void> {
  restoring.value = true;
  error.value = "";
  try {
    exam.value = await examApi.restore(id);
    confirmRestore.value = false;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    restoring.value = false;
  }
}

// ---------- Page-level ops: submit -> optimistic update -> poll detail until the task completes ----------

/** Poll every 2s x 30 (60s): covers the worker's 30s fetch cycle + headroom. */
const PAGES_OPS_POLL_MS = 2000;
const PAGES_OPS_MAX_ATTEMPTS = 30;

function sleepOps(ms: number): Promise<void> {
  return new Promise((resolve) => {
    pagesOpsTimer = setTimeout(() => {
      pagesOpsTimer = null;
      resolve();
    }, ms);
  });
}

/** adoptServer swaps data + page position only, never the edit form. */
function adoptServer(detail: ExamDetail): void {
  exam.value = detail;
  if (pageIndex.value >= detail.pages.length) {
    pageIndex.value = Math.max(0, detail.pages.length - 1);
  }
}

/**
 * applyOptimistic — redraw the page area in target order right after the 202.
 * Existing pages reorder/remove in place; new pages get negative-id placeholders
 * (ensurePageImage skips <=0, no doomed requests); server truth swaps in on
 * completion. Deleted blobs reclaimed on the spot.
 */
function applyOptimistic(payload: PageOpsPayload): void {
  const current = exam.value;
  if (current === null) return;
  const byId = new Map<number, PageSummary>(current.pages.map((p) => [p.id, p]));
  const next: PageSummary[] = [];
  payload.final_order.forEach((entry, idx) => {
    if (typeof entry === "number") {
      const p = byId.get(entry);
      if (p !== undefined) next.push({ ...p, page_index: idx + 1 });
      return;
    }
    next.push({ id: -(idx + 1), page_index: idx + 1, sha256: "" });
  });
  const keptIds = new Set(
    payload.final_order.filter((entry): entry is number => typeof entry === "number"),
  );
  for (const p of current.pages) {
    if (keptIds.has(p.id)) continue;
    const url = pageURLs.get(p.id);
    if (url !== undefined) {
      URL.revokeObjectURL(url);
      pageURLs.delete(p.id);
    }
  }
  exam.value = { ...current, pages: next, page_count: next.length };
  if (pageIndex.value >= next.length) pageIndex.value = Math.max(0, next.length - 1);
}

/**
 * pagesOpsSettled — detail already at target state after completion: counts match +
 * target ids in relative order + new-page counts match. originalIds is required:
 * without it a not-yet-deleted page misclassifies as new.
 */
function pagesOpsSettled(
  detail: ExamDetail,
  expected: readonly (number | string)[],
  originalIds: ReadonlySet<number>,
): boolean {
  if (detail.pages.length !== expected.length) return false;
  const wantedExisting = expected.filter((e): e is number => typeof e === "number");
  const wantedNew = expected.length - wantedExisting.length;
  let matched = 0;
  let unknown = 0;
  for (const p of detail.pages) {
    if (wantedExisting.includes(p.id)) {
      if (p.id !== wantedExisting[matched]) return false;
      matched += 1;
    } else if (originalIds.has(p.id)) {
      return false; // The to-be-deleted existing page is still on the server: the task has not finished
    } else {
      unknown += 1;
    }
  }
  return matched === wantedExisting.length && unknown === wantedNew;
}

async function savePagesOps(payload: PageOpsPayload, files: File[]): Promise<void> {
  pagesOpsSaving.value = true;
  pagesOpsMsg.value = "";
  try {
    // originalIds snapshots before the optimistic redraw (which swaps pages).
    const originalIds = new Set(
      (exam.value?.pages ?? []).map((p) => p.id),
    );
    const res = await examApi.opsPages(id, payload, files);
    pagesEditing.value = false;
    pagesOpsTaskId = res.task_id;
    pagesOpsPending.value = true;
    applyOptimistic(payload);
    void pollPagesOps(payload.final_order, originalIds);
  } catch (e) {
    // On failure keep editing state (selection intact for fix-and-resubmit).
    pagesOpsOk.value = false;
    pagesOpsMsg.value = e instanceof Error ? e.message : String(e);
  } finally {
    pagesOpsSaving.value = false;
  }
}

async function pollPagesOps(
  expected: readonly (number | string)[],
  originalIds: ReadonlySet<number>,
): Promise<void> {
  const me = ++pagesOpsSeq;
  for (
    let attempt = 0;
    attempt < PAGES_OPS_MAX_ATTEMPTS && pagesOpsSeq === me && pagesOpsPending.value;
    attempt += 1
  ) {
    if (attempt > 0) await sleepOps(PAGES_OPS_POLL_MS);
    if (pagesOpsSeq !== me || !pagesOpsPending.value) return;
    let detail: ExamDetail;
    try {
      detail = await examApi.get(id);
    } catch {
      continue; // Network jitter continues by tick without resetting the tick; the count keeps advancing, the 60s cap unchanged.
    }
    if (pagesOpsSeq !== me) return;
    if (pagesOpsSettled(detail, expected, originalIds)) {
      adoptServer(detail);
      pagesOpsPending.value = false;
      pagesOpsOk.value = true;
      pagesOpsMsg.value = "页已更新";
      return;
    }
  }
  if (pagesOpsSeq === me && pagesOpsPending.value) await pagesOpsTimeout();
}

/**
 * pagesOpsTimeout — 60s without target state: query the task once for the reason,
 * always roll back (timeout ≠ failure, no direct error).
 */
async function pagesOpsTimeout(): Promise<void> {
  let hint = "";
  if (!isKid.value) {
    // /api/tasks is requireParent: kid side would 40100-kick, so it only rolls back.
    try {
      const task = await settingsApi.task(pagesOpsTaskId);
      if (task.status === TASK_STATUS.failed) hint = `：${task.last_error}`;
    } catch {
      // A failed task-detail fetch doesn't block the rollback.
    }
  }
  let rolledBack = false;
  try {
    adoptServer(await examApi.get(id));
    rolledBack = true;
  } catch {
    // Detail unreachable too: keep the optimistic view, prompt manual refresh.
  }
  pagesOpsPending.value = false;
  pagesOpsOk.value = false;
  pagesOpsMsg.value = rolledBack
    ? `页整理未在 1 分钟内完成${hint}，已按服务器状态刷新`
    : `页整理未在 1 分钟内完成${hint}，请刷新页面查看`;
}

/**
 * The kid-name mapping needs the global list: on deep links Layout may not have loaded
 * it yet, so fetch once more. Kid side never fetches /api/kids (requireParent would
 * kick); ownership shows "me".
 */
onMounted(async () => {
  if (!isKid.value && kidStore.list.length === 0) await kidStore.fetchList();
  await load();
});
</script>
