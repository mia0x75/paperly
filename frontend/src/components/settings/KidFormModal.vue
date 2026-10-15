<template>
  <Modal
    :show="show"
    :title="mode === 'create' ? '新登记孩子' : '编辑孩子'"
    flex-body
    @close="close"
  >
    <!-- flex-1 + flex-col: the form fills the (non-scrolling) modal body with the fields
         pinned. When the subject list is expanded it is the only flexible region, so it
         scrolls alone and the fields above never scroll out of view. -->
    <div class="flex min-h-0 flex-1 flex-col gap-4">
      <label class="flex flex-col gap-1">
        <span class="text-muted-foreground">
          姓名<span class="text-destructive">*</span>
        </span>
        <input v-model="form.name" type="text" :class="[FIELD_BASE, 'h-9']" placeholder="请输入学生姓名" />
      </label>

      <div class="flex flex-col gap-4 sm:flex-row sm:gap-3">
        <label class="flex flex-1 flex-col gap-1">
          <span class="text-muted-foreground">
            年级<span class="text-destructive">*</span>
          </span>
          <Select v-model="form.grade" :options="GRADE_OPTIONS" @change="onGradeChange" />
        </label>
        <label class="flex flex-1 flex-col gap-1">
          <span class="text-muted-foreground">性别</span>
          <Select v-model="form.gender" :options="GENDER_OPTIONS" />
        </label>
      </div>

      <label v-if="mode === 'create'" class="flex flex-col gap-1">
        <span class="text-muted-foreground">访问 PIN（4-6 位数字）</span>
        <input v-model="form.pin" type="text" :class="[FIELD_BASE, 'h-9']" placeholder="4-6 位数字（孩子首次走邀请链接设置）" />
      </label>

      <!-- Subjects: the kid's actual study set (kid_subjects). Mandatory subjects are force-checked;
           read the grade/track only through the shared mask helpers so the defaults match the backend.
           Collapsed by default: the grid is the tallest block and would otherwise push the footer
           below 90vh. The default selection is still computed while collapsed, so submitting
           without expanding keeps the grade/track defaults. Expanded, this block is the only
           flexible region (flex-auto + min-h-0), so its grid scrolls internally while the header
           and the fields above stay put. -->
      <div class="rounded-lg border p-4" :class="subjectsExpanded && 'flex min-h-0 flex-auto flex-col'">
        <div class="flex shrink-0 flex-wrap items-center justify-between gap-2">
          <button
            type="button"
            class="flex items-center gap-1.5 text-sm font-medium"
            :aria-expanded="subjectsExpanded"
            @click="subjectsOpen = !subjectsOpen"
          >
            <span>实际在读学科</span>
            <span class="text-muted-foreground text-xs">
              {{ subjectsLoading ? "加载中…" : `已选 ${selected.size} 门` }}
            </span>
            <Icon
              :name="subjectsExpanded ? 'expand_less' : 'expand_more'"
              :size="18"
              class="text-muted-foreground shrink-0"
            />
          </button>
          <div
            v-if="trackEnabled"
            class="bg-muted/40 inline-flex items-center rounded-md border p-0.5"
            role="group"
            aria-label="文理科"
          >
            <Button
              v-for="t in TRACK_BUTTONS"
              :key="t.value"
              size="sm"
              class="h-7 px-3"
              :variant="form.track === t.value ? 'default' : 'ghost'"
              @click="setTrack(t.value)"
            >
              {{ t.label }}
            </Button>
          </div>
        </div>

        <p v-if="subjectsError" class="text-destructive mt-3 shrink-0 text-sm">{{ subjectsError }}</p>
        <div v-else-if="subjectsLoading && subjectsExpanded" class="mt-3 min-h-0 flex-auto space-y-2 overflow-y-auto pr-1">
          <div v-for="i in 4" :key="i" class="bg-muted h-6 animate-pulse rounded" />
        </div>
        <div v-else-if="subjectsExpanded" class="mt-3 min-h-[8rem] flex-auto overflow-y-auto px-px pr-1">
          <div class="grid grid-cols-3 gap-x-3 gap-y-1.5">
            <label
              v-for="s in subjects"
              :key="s.id"
              class="flex items-center gap-2 text-sm"
              :class="isMandatory(s) ? 'opacity-80' : 'cursor-pointer'"
            >
              <input
                type="checkbox"
                class="accent-primary size-4"
                :checked="selected.has(s.id)"
                :disabled="isMandatory(s)"
                @change="toggle(s.id, ($event.target as HTMLInputElement).checked)"
              />
              <span class="truncate">
                {{ s.name }}<span v-if="isMandatory(s)" class="text-destructive">*</span>
              </span>
            </label>
            <p v-if="subjects.length === 0" class="text-muted-foreground col-span-3 text-sm">暂无科目</p>
          </div>
        </div>
      </div>

      <p v-if="error" class="text-destructive m-0 text-sm">{{ error }}</p>
    </div>

    <template #footer>
      <Button :loading="saving" :disabled="!canSubmit()" class="w-full sm:w-auto" @click="submit">
        {{ mode === "create" ? "创建" : "保存" }}
      </Button>
      <Button variant="outline" :disabled="saving" class="w-full sm:w-auto" @click="close">取消</Button>
    </template>
  </Modal>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
import Button from "../ui/Button.vue";
import Icon from "../Icon.vue";
import Modal from "../Modal.vue";
import Select from "../ui/Select.vue";
import { GRADE_OPTIONS } from "../../lib/grade";
import { TRACK_MIN_GRADE, subjectVisibleForGrade, subjectVisibleForTrack } from "../../lib/subject";
import { FIELD_BASE, type SelectOption } from "../../lib/ui";
import { KID_GENDER, KID_TRACK, KID_TRACK_OPTIONS } from "../../api/types";
import { subjectApi, type Subject } from "../../api/subject";

/** Initial value (from GET /kids/:id when editing). */
export interface KidFormValue {
  name: string;
  grade: number;
  /** 1 = male / 0 = female / -1 = not set. */
  gender: number;
  /** 0 undecided / 1 arts / 2 science. */
  track: number;
  /** The kid's current kid_subjects subject ids (edit mode; absent when creating). */
  subjectIds?: number[];
}

/**
 * KidFormModal — dialog for registering / editing a kid.
 *
 * Add/edit stays in a dialog (no page navigation to break context). The subject
 * block edits the kid_subjects whitelist with the profile — one atomic write.
 * Mandatory subjects are force-checked; the parent picks the rest.
 */
const props = withDefaults(
  defineProps<{
    show: boolean;
    mode: "create" | "edit";
    initial?: KidFormValue | null;
    saving?: boolean;
    error?: string;
  }>(),
  { saving: false, error: "", initial: null },
);

const emit = defineEmits<{
  close: [];
  submit: [{ name: string; grade: number; gender: number; track: number; subjectIds: number[]; pin?: string }];
}>();

const GENDER_OPTIONS: readonly SelectOption<number>[] = [
  { value: -1, label: "未填写" },
  { value: KID_GENDER.MALE, label: "男" },
  { value: KID_GENDER.FEMALE, label: "女" },
];

const TRACK_BUTTONS: readonly { value: number; label: string }[] = KID_TRACK_OPTIONS.filter(
  (o) => o.value !== KID_TRACK.UNDECIDED,
);

const form = reactive({ name: "", grade: 0, gender: -1, track: KID_TRACK.UNDECIDED, pin: "" });

const trackEnabled = computed(() => form.grade >= TRACK_MIN_GRADE);

const subjects = ref<Subject[]>([]);
const selected = ref<Set<number>>(new Set());
const subjectsLoading = ref(false);
const subjectsError = ref("");
// Collapsed by default (the grid is the tallest block); auto-expands when loading the
// subject list fails, so the error can never hide behind the collapsed header.
const subjectsOpen = ref(false);
const subjectsExpanded = computed(() => subjectsOpen.value || subjectsError.value !== "");
// Set once the parent hand-picks: auto-recompute must stop, or it clobbers the pick.
const touched = ref(false);

// Reset from initial on every open (never show the previous kid's name).
watch(
  () => [props.show, props.initial] as const,
  async () => {
    if (!props.show) return;
    form.name = props.initial?.name ?? "";
    form.grade = props.initial?.grade ?? 0;
    form.gender = props.initial?.gender ?? -1;
    form.track = props.initial?.track ?? KID_TRACK.UNDECIDED;
    form.pin = "";
    touched.value = false;
    subjectsOpen.value = false;
    await loadSubjects();
    resetSelection();
  },
  { immediate: true },
);

async function loadSubjects(): Promise<void> {
  if (subjects.value.length > 0) return;
  subjectsLoading.value = true;
  subjectsError.value = "";
  try {
    const list = await subjectApi.list();
    // Defensive: a failed/empty payload must not become a null `subjects` (the template and the
    // default-selection logic iterate it, and a null would abort rendering of the whole dialog).
    subjects.value = Array.isArray(list) ? list : [];
  } catch (e) {
    subjectsError.value = e instanceof Error ? e.message : String(e);
  } finally {
    subjectsLoading.value = false;
  }
}

function resetSelection(): void {
  if (props.mode === "edit") {
    setSelection(props.initial?.subjectIds ?? []);
  } else {
    applyDefaults();
  }
}

function applyDefaults(): void {
  const ids = subjects.value
    .filter((s) => subjectVisibleForGrade(s, form.grade) && subjectVisibleForTrack(s, form.track, form.grade))
    .map((s) => s.id);
  setSelection(ids);
}

/** Force-include mandatory subjects (backend rule). */
function setSelection(ids: number[]): void {
  const next = new Set(ids);
  for (const s of subjects.value) {
    if (s.mandatory === 1) next.add(s.id);
  }
  selected.value = next;
}

function isMandatory(s: Subject): boolean {
  return s.mandatory === 1;
}

function toggle(id: number, on: boolean): void {
    // Mandatory can't toggle off (UI disables it too; this guards programmatic changes).
    const s = subjects.value.find((x) => x.id === id);
    if (s && isMandatory(s) && !on) return;
    const next = new Set(selected.value);
    if (on) next.add(id);
    else next.delete(id);
    selected.value = next;
    touched.value = true;
  }

function onGradeChange(): void {
  if (!touched.value) applyDefaults();
}

/** Track toggle: active clears to undecided; re-derives defaults (switching arts/science must re-pick). */
function setTrack(value: number): void {
  form.track = form.track === value ? KID_TRACK.UNDECIDED : value;
  if (!touched.value) applyDefaults();
}

function close(): void {
  emit("close");
}

function submit(): void {
  if (!canSubmit()) return;
  emit("submit", {
    name: form.name.trim(),
    grade: form.grade,
    gender: form.gender,
    track: form.track,
    subjectIds: [...selected.value],
    pin: form.pin,
  });
}

function canSubmit(): boolean {
  if (form.name.trim() === "" || form.grade <= 0 || props.saving) return false;
  // Block submit until the dictionary loads (an empty whitelist would only surface a backend error).
  return !subjectsLoading.value && subjectsError.value === "" && subjects.value.length > 0;
}
</script>
