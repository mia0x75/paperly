<template>
  <Modal :show="show" :title="`「${subject?.name ?? ''}」可见范围`" @close="close">
    <p class="text-muted-foreground mt-0 mb-4 text-sm">
      <span v-if="isSystem">系统内置科目的学段与文理信息为只读，不可更改。</span>
      <span v-else>勾选该科目出现的学段与文理分科；全部取消则该科目在试卷筛选与雷达图中隐藏。</span>
    </p>

    <p class="text-muted-foreground mb-2 text-xs font-medium">学段</p>
    <div class="flex flex-col gap-3">
      <label
        v-for="stage in SUBJECT_STAGE_OPTIONS"
        :key="stage.bit"
        class="hover:bg-accent flex items-center gap-3 rounded-md px-3 py-2 transition-colors"
        :class="isSystem ? 'cursor-default' : 'cursor-pointer'"
      >
        <input
          type="checkbox"
          class="accent-primary size-4"
          :checked="(mask & stage.bit) !== 0"
          :disabled="isSystem"
          @change="toggle(stage.bit, ($event.target as HTMLInputElement).checked)"
        />
        <span class="text-sm font-medium">{{ stage.label }}</span>
      </label>
    </div>

    <!-- Track bits (high 2): core subjects carry both arts+science; track-specific
         subjects (physics/chemistry → science, history/geography → arts) turn the matching bit on. -->
    <p class="text-muted-foreground mt-5 mb-2 text-xs font-medium">文理</p>
    <div class="flex flex-col gap-3">
      <label
        v-for="track in SUBJECT_TRACK_OPTIONS"
        :key="track.bit"
        class="hover:bg-accent flex items-center gap-3 rounded-md px-3 py-2 transition-colors"
        :class="isSystem ? 'cursor-default' : 'cursor-pointer'"
      >
        <input
          type="checkbox"
          class="accent-primary size-4"
          :checked="(mask & track.bit) !== 0"
          :disabled="isSystem"
          @change="toggle(track.bit, ($event.target as HTMLInputElement).checked)"
        />
        <span class="text-sm font-medium">{{ track.label }}</span>
      </label>
    </div>

    <p v-if="error" class="text-destructive mt-3 text-sm">{{ error }}</p>

    <template #footer>
      <Button v-if="!isSystem" :disabled="loading" class="w-full sm:w-auto" @click="save">{{ loading ? "保存中…" : "保存" }}</Button>
      <Button variant="outline" class="w-full sm:w-auto" @click="close">{{ isSystem ? "关闭" : "取消" }}</Button>
    </template>
  </Modal>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import Button from "../ui/Button.vue";
import Modal from "../Modal.vue";
import type { SubjectSummary } from "../../api/types";
import { SUBJECT_STAGE_OPTIONS, SUBJECT_TRACK_OPTIONS } from "../../lib/subject";

/**
 * SubjectMaskDialog — edits one subject's visibility mask as five checkboxes:
 * three stage bits (low 3) and two track bits (high 2).
 *
 * Local `mask` re-seeds from the subject on every open (never show the previous
 * selection). 0 (all unchecked) is valid: hides the subject everywhere.
 * System-preset subjects (source=1) are read-only (Close only, no Save).
 */
const SOURCE_SYSTEM = 1;

const props = defineProps<{
  show: boolean;
  subject: SubjectSummary | null;
  loading?: boolean;
  error?: string;
}>();
const emit = defineEmits<{ close: []; submit: [number] }>();

const mask = ref(0);

const isSystem = computed(() => props.subject?.source === SOURCE_SYSTEM);

watch(
  () => props.show,
  (show) => {
    if (show) mask.value = props.subject?.mask ?? 0;
  },
  { immediate: true },
);

function toggle(bit: number, on: boolean): void {
  if (isSystem.value) return;
  mask.value = on ? mask.value | bit : mask.value & ~bit;
}

function close(): void {
  emit("close");
}

function save(): void {
  emit("submit", mask.value);
}
</script>
