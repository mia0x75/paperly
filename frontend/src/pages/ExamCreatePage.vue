<template>
  <div class="flex flex-col gap-6">
    <p v-if="error" class="border-destructive/50 bg-destructive/10 text-destructive rounded-md border px-3 py-2 text-sm">
      {{ error }}
    </p>

    <!-- Duplicate exam: guide the user to the existing one instead of a generic error. -->
    <div v-if="duplicateId" class="rounded-md border px-3 py-2 text-sm">
      <p class="m-0">该试卷已存在（内容相同）。</p>
      <Button variant="outline" size="sm" class="mt-2" @click="router.push(`${listHref}/${duplicateId}`)">
        查看已有试卷
      </Button>
    </div>

    <!-- ═══ Metadata config ═══ -->
    <section class="bg-card text-card-foreground flex flex-col gap-4 rounded-xl border p-6 shadow-sm">
      <h2 class="text-base font-semibold tracking-tight">录入新试卷</h2>

      <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <!-- Owning kid (parent side): choose on this page which kid to upload for;
             the choice is written to kidStore.selectedId and persisted to
             localStorage as the default kid for the next entry. The kid side
             derives kidId from its own session (defaults to self) and does not
             show this field. -->
        <label v-if="!hideOwner && !isKidRoute" class="flex flex-col gap-1 text-sm">
          <span class="text-muted-foreground">归属孩子 <span class="text-destructive">*</span></span>
          <Select v-model="selectedKidId" :options="kidOptions" placeholder="请选择孩子" />
        </label>

        <div class="flex flex-col gap-1 text-sm">
          <span class="text-muted-foreground flex items-center justify-between gap-2">
            <span>科目 <span class="text-destructive">*</span></span>
            <!-- Subjects come from the owning kid's kid_subjects; a kid with none configured
                 shows an empty list (no mask fallback, no "show all" escape hatch). -->
          </span>
          <Select v-model="form.subjectId" :options="subjectOptions" placeholder="请选择科目" />
        </div>

        <label class="flex flex-col gap-1 text-sm">
          <span class="text-muted-foreground">年级 <span class="text-destructive">*</span></span>
          <Select
            v-model="form.grade"
            :options="gradeOptions"
            placeholder="请选择年级"
            @change="gradeTouched = true"
          />
        </label>

        <label class="flex flex-col gap-1 text-sm">
          <span class="text-muted-foreground">试卷类型 <span class="text-destructive">*</span></span>
          <Select v-model="form.examType" :options="examTypeOptions" placeholder="请选择类型" />
        </label>

        <label class="flex flex-col gap-1 text-sm">
          <span class="text-muted-foreground">考试日期 <span class="text-destructive">*</span></span>
          <DatePicker v-model="form.examDate" />
        </label>

        <label class="flex flex-col gap-1 text-sm sm:col-span-2">
          <span class="text-muted-foreground">
            试卷名称
            <span class="text-muted-foreground/70 text-xs">（按规则自动生成，可修改）</span>
          </span>
          <input
            :value="form.title"
            type="text"
            :class="[FIELD_BASE, 'h-9']"
            placeholder="选择科目 / 年级 / 类型 / 日期后自动生成"
            @input="onTitleInput"
          />
        </label>
      </div>
    </section>

    <!-- ═══ Image upload & ordering ═══ -->
    <section class="bg-card text-card-foreground flex flex-col gap-4 rounded-xl border p-6 shadow-sm">
      <h2 class="text-base font-semibold tracking-tight">试卷图片上传与排序</h2>

      <div
        class="flex cursor-pointer flex-col items-center justify-center rounded-xl border border-dashed px-6 py-8 text-center transition-colors"
        :class="dragActive ? 'border-primary bg-primary/5' : 'hover:bg-muted/50'"
        @click="openPicker"
        @dragover.prevent="dragActive = true"
        @dragenter.prevent="dragActive = true"
        @dragleave.prevent="dragActive = false"
        @drop.prevent="onDrop"
      >
        <Icon name="upload" :size="28" class="text-muted-foreground" />
        <p class="mt-2 text-sm">点击选择图片，或直接拖拽图片到此处</p>
        <p class="text-muted-foreground mt-1 text-xs">
          支持多张（手机连拍 / 电脑多选）；上传后统一压缩为 WebP，长边不超过 5120px
        </p>
        <input
          ref="fileInput"
          type="file"
          accept="image/jpeg,image/png,image/webp"
          multiple
          class="hidden"
          @change="onPick"
        />
      </div>

      <div v-if="images.length > 0" class="flex flex-col gap-3">
        <div class="text-muted-foreground text-sm">
          已添加 {{ images.length }} 页 · 可上下调整页码顺序（页码即页序）
        </div>
        <ul class="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
          <li
            v-for="(img, i) in images"
            :key="img.key"
            class="flex flex-col overflow-hidden rounded-lg border"
          >
            <div class="bg-muted flex h-32 items-center justify-center overflow-hidden">
              <img :src="img.previewUrl" :alt="img.name" class="max-h-full max-w-full object-contain" />
            </div>
            <div class="flex items-center gap-1 px-2 py-1.5">
              <span class="text-muted-foreground text-xs tabular-nums">P{{ i + 1 }}</span>
              <span class="text-muted-foreground truncate text-xs" :title="img.name">{{ img.name }}</span>
              <!-- min-h-11/min-w-11: 44px touch targets on mobile (same pattern as
                   ExamPagesEditor's reorder buttons). -->
              <div class="ml-auto flex items-center gap-0.5">
                <button
                  type="button"
                  class="hover:bg-accent inline-flex min-h-11 min-w-11 items-center justify-center rounded transition-colors disabled:opacity-30"
                  :disabled="i === 0"
                  title="上移"
                  @click="move(i, -1)"
                >
                  <Icon name="expand_less" :size="16" />
                </button>
                <button
                  type="button"
                  class="hover:bg-accent inline-flex min-h-11 min-w-11 items-center justify-center rounded transition-colors disabled:opacity-30"
                  :disabled="i === images.length - 1"
                  title="下移"
                  @click="move(i, 1)"
                >
                  <Icon name="expand_more" :size="16" />
                </button>
                <button
                  type="button"
                  class="hover:bg-accent text-destructive inline-flex min-h-11 min-w-11 items-center justify-center rounded transition-colors"
                  title="移除"
                  @click="remove(i)"
                >
                  <Icon name="close" :size="16" />
                </button>
              </div>
            </div>
          </li>
        </ul>
      </div>

      <!-- Upload progress: fetch cannot report byte progress, so show only an
           indeterminate state; do not fake a percentage. -->
      <div v-if="uploading" class="bg-muted h-1.5 w-full overflow-hidden rounded-full">
        <div class="bg-progress h-full w-1/3 animate-pulse rounded-full" />
      </div>
    </section>

    <div class="flex items-center gap-3">
      <Button variant="outline" :disabled="uploading" @click="router.push(listHref)">取消</Button>
      <Button
          variant="outline"
          icon="save"
          :loading="uploading"
          :disabled="!canSubmit"
          @click="submit"
        >
        新增
      </Button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import Button from "../components/ui/Button.vue";
import { FIELD_BASE } from "../lib/ui";
import Icon from "../components/Icon.vue";
import Select from "../components/ui/Select.vue";
import DatePicker from "../components/ui/DatePicker.vue";
import { ApiError } from "../api/client";
import { examApi } from "../api/exam";
import { kidApi } from "../api/kid";
import { subjectApi, type Subject } from "../api/subject";
import { useKidStore } from "../stores/kid";
import { useKidSessionStore } from "../stores/kidSession";
import { GRADE_OPTIONS } from "../lib/grade";
import { EXAM_TYPE_OPTIONS, genTitle } from "../lib/examTitle";
import { toDateOnly } from "../lib/date";
import { KID_STATUS } from "../api/types";
import type { SelectOption } from "../lib/ui";

/**
 * ExamCreatePage: create a new exam (parent `/exams/upload`, kid `/kid/exams/upload`).
 *
 * Ownership: parent side picks the owning kid (written to kidStore.selectedId +
 * localStorage as next entry's default); kid side derives from its session and hides
 * the selector. Upload keeps add order (backend uses it as page_index); thumbnails
 * are object URLs, all revoked on unmount.
 */
const props = withDefaults(
  defineProps<{
    /** Owning kid id; 0 = not selected (submit disabled). */
    kidId?: number;
    hideOwner?: boolean;
  }>(),
  { kidId: 0, hideOwner: false },
);

const router = useRouter();
const route = useRoute();

/** Kid-side route: own session affiliation, no ownership strip, back to /kid/exams. */
const isKidRoute = computed(() => route.meta.audience === "kid");
const listHref = computed(() => (isKidRoute.value ? "/kid/exams" : "/exams"));

interface PickedImage {
  key: number;
  name: string;
  /** Original file (best-effort compressed). */
  file: File;
  /** Object URL for preview. */
  previewUrl: string;
}

const kidStore = useKidStore();
const kidSession = useKidSessionStore();

const fileInput = ref<HTMLInputElement | null>(null);
const images = ref<PickedImage[]>([]);
const dragActive = ref(false);
const uploading = ref(false);
const error = ref("");
const duplicateId = ref<number | null>(null);

const subjects = ref<Subject[]>([]);

const gradeOptions: readonly SelectOption<number>[] = GRADE_OPTIONS;
const examTypeOptions: readonly SelectOption<number>[] = EXAM_TYPE_OPTIONS;

const form = reactive({
  subjectId: 0,
  grade: 0,
  examType: 0,
  /**
   * Exam date defaults to today: parents usually create right after an exam, so empty
   * would cost one extra calendar tap. Manual edits stick (v-model overwrites).
   */
  examDate: toDateOnly(new Date()),
  title: "",
  /** Title manually edited: auto-generation stops overwriting. */
  titleTouched: false,
});

const subjectOptions = computed<readonly SelectOption<number>[]>(() =>
  subjects.value.map((s) => ({ value: s.id, label: s.name })),
);

const gradeTouched = ref(false);

/**
 * applyOwnerDefaults prefills the grade with the owning kid's grade — silently with a
 * single kid (picking from 12 options every time is noise), explicitly on switch. Kid
 * side takes the session grade (no /api/kids fetch). Never clobbers a manual edit.
 */
function applyOwnerDefaults(kidID: number): void {
  if (gradeTouched.value) return;
  if (isKidRoute.value) {
    if (kidSession.grade > 0) form.grade = kidSession.grade;
    return;
  }
  const owner = kidStore.list.find((k) => k.id === kidID);
  if (owner && owner.grade > 0) form.grade = owner.grade;
}

let keySeq = 0;

/** Effective ownership: kid side its session kid; parent side props.kidId, else selected kid. */
const effectiveKidId = computed(() => {
  if (isKidRoute.value) return kidSession.kidId ?? 0;
  return props.kidId > 0 ? props.kidId : (kidStore.selectedId ?? 0);
});

/** Owning-kid options: active kids only. */
const kidOptions = computed<readonly SelectOption<number>[]>(() =>
  kidStore.list
    .filter((k) => k.status === KID_STATUS.ACTIVE)
    .map((k) => ({ value: k.id, label: k.name })),
);

/**
 * Owning-kid selection (parent side). get: effective ownership; set: persist to
 * kidStore.selectedId + localStorage as next entry's default.
 */
const selectedKidId = computed<number | null>({
  get: () => effectiveKidId.value,
  set: (v) => {
    if (v !== null) kidStore.select(v);
  },
});

const canSubmit = computed(
  () =>
    effectiveKidId.value > 0 &&
    form.subjectId > 0 &&
    form.grade > 0 &&
    form.examType > 0 &&
    form.examDate !== "" &&
    images.value.length > 0,
);

// ── Title auto-generation ────────────────────────────────────
const subjectName = computed(
  () => subjects.value.find((s) => s.id === form.subjectId)?.name ?? "",
);

watch(
  () => [form.subjectId, form.grade, form.examType, form.examDate] as const,
  () => {
    if (form.titleTouched) return;
    form.title = genTitle(form.examDate, form.grade, subjectName.value, form.examType);
  },
);

// On owner change: prefill grade (unless edited), reload per-kid subjects, reset stale subject pick.
watch(
  effectiveKidId,
  async (id) => {
    if (id <= 0) return;
    form.subjectId = 0;
    await loadSubjects();
    applyOwnerDefaults(id);
  },
);

/**
 * Typing marks the title "manually edited", stopping auto-generation.
 *
 * Don't mark via watch(form.title): the empty string auto-generation writes while
 * metadata is incomplete would lock the title permanently.
 */
function onTitleInput(event: Event): void {
  form.title = (event.target as HTMLInputElement).value;
  form.titleTouched = true;
}

// ── Images ──────────────────────────────────────────────────
function openPicker(): void {
  if (!uploading.value) fileInput.value?.click();
}

function onPick(event: Event): void {
  const input = event.target as HTMLInputElement;
  addFiles(Array.from(input.files ?? []));
  input.value = "";
}

function onDrop(event: DragEvent): void {
  dragActive.value = false;
  addFiles(Array.from(event.dataTransfer?.files ?? []));
}

/** Validate type and push; compression is server-side (EXIF + WebP). */
async function addFiles(files: File[]): Promise<void> {
  error.value = "";
  duplicateId.value = null;
  for (const f of files) {
    if (!f.type.startsWith("image/")) {
      error.value = "仅支持图片文件（JPG / PNG / WebP）";
      continue;
    }
    images.value.push({
      key: ++keySeq,
      name: f.name,
      file: f,
      previewUrl: URL.createObjectURL(f),
    });
  }
}

function move(index: number, delta: number): void {
  const target = index + delta;
  if (target < 0 || target >= images.value.length) return;
  const arr = images.value;
  const [item] = arr.splice(index, 1);
  if (item !== undefined) arr.splice(target, 0, item);
}

function remove(index: number): void {
  const arr = images.value;
  const [item] = arr.splice(index, 1);
  if (item !== undefined) URL.revokeObjectURL(item.previewUrl);
}

/** Reclaim all object URLs on page unmount. */
function revokeAll(): void {
  for (const img of images.value) URL.revokeObjectURL(img.previewUrl);
  images.value = [];
}

// ── Submit ──────────────────────────────────────────────────
function parseExistingId(detail: string): number | null {
  try {
    const v = JSON.parse(detail) as { existing_id?: number };
    return typeof v.existing_id === "number" ? v.existing_id : null;
  } catch {
    return null;
  }
}

async function submit(): Promise<void> {
  if (!canSubmit.value) return;
  uploading.value = true;
  error.value = "";
  duplicateId.value = null;
  try {
    // Field order = page order (current image order maps to file order).
    const files = images.value.map((i) => i.file);
    const res = await examApi.uploadMany(files, {
      title: form.title.trim() || undefined,
      subject_id: form.subjectId,
      grade: form.grade,
      exam_type: form.examType,
      exam_date: form.examDate,
      kid_id: effectiveKidId.value,
    });
    revokeAll();
    await router.push(`${listHref.value}/${res.id}`);
  } catch (e) {
    if (e instanceof ApiError && e.code === 40022) {
      duplicateId.value = parseExistingId(e.detail);
    } else {
      error.value = e instanceof Error ? e.message : String(e);
    }
  } finally {
    uploading.value = false;
  }
}

/**
 * loadSubjects loads the owning kid's subjects (kid_subjects). No mask fallback:
 * unconfigured yields empty. Silent on error — the page still renders, submit
 * stays disabled until a subject is chosen.
 */
async function loadSubjects(): Promise<void> {
  const id = effectiveKidId.value;
  if (id <= 0) {
    subjects.value = [];
    return;
  }
  try {
    subjects.value = (await subjectApi.list(undefined, undefined, id)) ?? [];
  } catch {
    subjects.value = [];
  }
}

onMounted(async () => {
  try {
    // Deep-link entry may precede the layout: ensure the kid list is ready.
    // Kid side carries kidId in-session, no /api/kids fetch.
    if (!isKidRoute.value && kidStore.list.length === 0) {
      await kidStore.fetchList();
    }
    // Subjects need the kid list ready first (grade prefill depends on it).
    await loadSubjects();
    if (effectiveKidId.value > 0) applyOwnerDefaults(effectiveKidId.value);
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  }
});
</script>
