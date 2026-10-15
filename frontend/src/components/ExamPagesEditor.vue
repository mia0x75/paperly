<template>
  <div class="flex flex-col gap-4">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <p class="text-muted-foreground text-xs">
        拖动卡片或用上下按钮调页序；删除标记的页保存后移除。保存会生成一个后台任务，完成后本页自动刷新。
      </p>
      <Button variant="outline" size="sm" icon="add" :disabled="saving" @click="openPicker">
        添加页
      </Button>
      <input
        ref="picker"
        type="file"
        accept="image/*"
        multiple
        class="hidden"
        @change="onFileChange"
      />
    </div>

    <p v-if="notice !== ''" class="text-destructive text-xs">{{ notice }}</p>

    <div v-if="items.length > 0" class="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-4">
      <div
        v-for="row in positioned"
        :key="row.it.key"
        class="bg-background rounded-lg border p-2 transition-all"
        :class="row.deleted ? 'opacity-60' : 'hover:border-foreground/40 cursor-grab'"
        :draggable="!saving"
        @dragstart="onDragStart(row.it, $event)"
        @dragover.prevent
        @drop="onDrop(row.it)"
        @dragend="onDragEnd"
      >
        <div class="relative overflow-hidden rounded-md">
          <img
            v-if="previewOf(row.it) !== ''"
            :src="previewOf(row.it)"
            :alt="row.pos > 0 ? `第 ${row.pos} 页` : '待删除页'"
            class="bg-muted aspect-[3/4] w-full object-cover"
          />
          <div v-else class="bg-muted aspect-[3/4] w-full animate-pulse rounded-md" />
          <span
            class="bg-popover absolute left-1 top-1 rounded border px-1.5 py-0.5 text-xs font-medium tabular-nums"
          >
            {{ row.deleted ? "✕" : row.pos }}
          </span>
          <div
            v-if="row.deleted"
            class="absolute inset-0 flex items-center justify-center bg-black/40"
          >
            <span class="text-destructive text-xs font-semibold">将删除</span>
          </div>
        </div>
        <!-- 44px touch target: min-h/w-11 layered over Button's size-9. -->
        <div class="mt-2 flex items-center justify-between">
          <Button
            variant="ghost"
            size="icon"
            class="min-h-11 min-w-11"
            :disabled="saving"
            title="上移"
            @click="moveBy(row.it, -1)"
          >
            <Icon name="expand_less" :size="16" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            class="min-h-11 min-w-11"
            :disabled="saving"
            title="下移"
            @click="moveBy(row.it, 1)"
          >
            <Icon name="expand_more" :size="16" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            class="min-h-11 min-w-11"
            :class="row.deleted ? 'text-destructive' : ''"
            :disabled="saving"
            :title="row.it.pageId === null ? '移除这张新页' : row.it.deleted ? '取消删除' : '删除这页'"
            @click="toggleMark(row.it)"
          >
            <Icon name="delete" :size="16" />
          </Button>
        </div>
      </div>
    </div>

    <p v-else class="text-muted-foreground text-sm">还没有页——点「添加页」选入图片。</p>

    <div class="flex justify-end gap-3">
      <Button variant="ghost" :disabled="saving" @click="emit('close')">取消</Button>
      <Button icon="save" :loading="saving" :disabled="!canSave" @click="save">保存</Button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import Button from "./ui/Button.vue";
import Icon from "./Icon.vue";
import type { PageOpsPayload, PageSummary } from "../api/types";

/**
 * ExamPagesEditor -- page-level exam operations editor (delete / add / drag-reorder).
 *
 * Inlined in the gallery (not a dialog: the objects are the thumbnails; a dialog would
 * force cross-referencing two views). Native HTML5 DnD, no drag library — plus up/down
 * buttons, since DnD is unavailable on touch.
 *
 * Emits (payload, files) for the parent to submit; networking/polling/optimistic
 * updates live in the parent. This component only manages working state + validation.
 */
const MAX_PAGES_OPS = 50; // matches the backend planPagesOps limit

const props = defineProps<{
  /** Server's current pages (snapshotted into working state on open). */
  pages: readonly PageSummary[];
  /** Blob URLs of existing pages (cached by the parent). */
  imageUrls: ReadonlyMap<number, string>;
  /** Request in flight: disables all interaction. */
  saving: boolean;
}>();

const emit = defineEmits<{
  close: [];
  save: [payload: PageOpsPayload, files: File[]];
}>();

interface EditItem {
  /** Render key, also the drag-positioning key: existing `p:<id>`, new `n:<seq>`. */
  key: string;
  /** Existing page id; null for new pages. */
  pageId: number | null;
  file: File | null;
  previewUrl: string;
  /** Delete mark (existing pages only; new pages are removed directly). */
  deleted: boolean;
}

const items = ref<EditItem[]>(
  props.pages.map((p) => ({ key: `p:${p.id}`, pageId: p.id, file: null, previewUrl: "", deleted: false })),
);
/** New-page key sequence (uniqueness only; final_order indexes are re-numbered at save). */
let newSeq = 0;
/** Order snapshot at open, for the "changed?" check. */
const initialKeys = items.value.map((it) => it.key);

const picker = ref<HTMLInputElement | null>(null);
const notice = ref("");
const dragKey = ref("");

function openPicker(): void {
  picker.value?.click();
}

function onFileChange(event: Event): void {
  const input = event.target;
  if (!(input instanceof HTMLInputElement)) return;
  const chosen = Array.from(input.files ?? []);
  input.value = "";
  if (chosen.length === 0) return;
  const room = MAX_PAGES_OPS - keptItems.value.length;
  if (room <= 0) {
    notice.value = `每份试卷最多 ${MAX_PAGES_OPS} 页`;
    return;
  }
  const accepted = chosen.slice(0, room);
  notice.value =
    accepted.length < chosen.length ? `超出页数上限，只添加了前 ${accepted.length} 张` : "";
  for (const file of accepted) {
    items.value.push({
      key: `n:${newSeq++}`,
      pageId: null,
      file,
      previewUrl: URL.createObjectURL(file),
      deleted: false,
    });
  }
}

const keptItems = computed(() =>
  items.value.filter((it) => it.deleted === false || it.pageId === null),
);

/** Final page number per card (shows 0 -> x while being deleted). */
const positioned = computed(() => {
  let pos = 0;
  return items.value.map((it) => {
    const deleted = it.pageId !== null && it.deleted;
    if (!deleted) pos += 1;
    return { it, deleted, pos };
  });
});

function toggleMark(it: EditItem): void {
  if (it.pageId === null) {
    // New pages are removed directly (revoke the preview blob here, don't wait for unmount).
    URL.revokeObjectURL(it.previewUrl);
    items.value = items.value.filter((x) => x.key !== it.key);
    notice.value = "";
    return;
  }
  if (!it.deleted) {
    // Backend requires non-empty final_order: count survivors (including new pages) before marking.
    const remaining = items.value.filter(
      (x) => x.key !== it.key && (x.deleted === false || x.pageId === null),
    ).length;
    if (remaining === 0) {
      notice.value = "至少保留 1 页";
      return;
    }
  }
  it.deleted = !it.deleted;
  notice.value = "";
}

function moveBy(it: EditItem, delta: -1 | 1): void {
  const from = items.value.findIndex((x) => x.key === it.key);
  const to = from + delta;
  if (from < 0 || to < 0 || to >= items.value.length) return;
  const moved = items.value.splice(from, 1)[0];
  if (moved === undefined) return;
  items.value.splice(to, 0, moved);
}

function onDragStart(it: EditItem, event: DragEvent): void {
  dragKey.value = it.key;
  if (event.dataTransfer !== null) {
    event.dataTransfer.effectAllowed = "move";
    // Firefox requires setData to start a drag (otherwise dragstart is ignored outright).
    event.dataTransfer.setData("text/plain", it.key);
  }
}

function onDrop(target: EditItem): void {
  const from = items.value.findIndex((x) => x.key === dragKey.value);
  const to = items.value.findIndex((x) => x.key === target.key);
  dragKey.value = "";
  if (from < 0 || to < 0 || from === to) return;
  const moved = items.value.splice(from, 1)[0];
  if (moved === undefined) return;
  items.value.splice(to, 0, moved);
}

function onDragEnd(): void {
  dragKey.value = "";
}

function previewOf(it: EditItem): string {
  if (it.pageId === null) return it.previewUrl;
  return props.imageUrls.get(it.pageId) ?? "";
}

/** Build the submit payload; null when invalid (empty order / fileless new page / over limit). */
function buildPlan(): { payload: PageOpsPayload; files: File[] } | null {
  const files: File[] = [];
  const finalOrder: (number | string)[] = [];
  const deletePageIds: number[] = [];
  for (const it of items.value) {
    if (it.pageId !== null) {
      if (it.deleted) {
        deletePageIds.push(it.pageId);
        continue;
      }
      finalOrder.push(it.pageId);
      continue;
    }
    if (it.file === null) return null;
    finalOrder.push(`new_${files.length}`);
    files.push(it.file);
  }
  if (finalOrder.length === 0 || finalOrder.length > MAX_PAGES_OPS) return null;
  return { payload: { delete_page_ids: deletePageIds, final_order: finalOrder }, files };
}

const changed = computed(() => {
  const kept = keptItems.value;
  if (kept.length !== initialKeys.length) return true;
  return kept.some((it, i) => it.key !== initialKeys[i]);
});

const canSave = computed(() => !props.saving && buildPlan() !== null && changed.value);

function save(): void {
  const plan = buildPlan();
  if (plan === null) {
    notice.value = keptItems.value.length === 0 ? "至少保留 1 页" : "整理内容不完整，无法保存";
    return;
  }
  notice.value = "";
  emit("save", plan.payload, plan.files);
}
</script>
