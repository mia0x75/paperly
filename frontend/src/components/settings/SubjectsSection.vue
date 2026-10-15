<template>
  <section id="section-subjects" class="bg-card text-card-foreground flex flex-col gap-4 rounded-xl border p-6 shadow-sm">
    <div class="flex items-center justify-between">
      <h2 class="text-base font-semibold tracking-tight">科目管理</h2>
      <Button variant="outline" icon="add" @click="addOpen = true">添加科目</Button>
    </div>

    <ErrorState v-if="error !== ''" :message="error" retry-label="重试" @retry="load" />
    <div v-else-if="loading" class="space-y-2">
      <div v-for="i in 3" :key="i" class="bg-muted animate-pulse h-11 rounded-lg" />
    </div>
    <EmptyState v-else-if="subjects.length === 0" icon="folder" title="暂无科目" />

    <ul v-else class="divide-border/60 divide-y">
      <!-- Row layout. Mobile (default): row 1 = name + source badge, row 2 = equal-width
           [学段][改名], row 3 = full-width destructive delete (self-built only). The stage
           is edited through the 学段 button, so no stage text is printed in the row.
           sm+ collapses back to a single inline action group pushed right by ml-auto. -->
      <li
        v-for="s in subjects"
        :key="s.id"
        class="flex flex-col gap-2 py-3 sm:flex-row sm:flex-wrap sm:items-center sm:gap-x-3 sm:py-2.5"
      >
        <!-- Row 1: name + source badge -->
        <div class="flex flex-wrap items-center gap-x-3 gap-y-2">
          <span class="font-medium">{{ s.name }}</span>
          <StatusBadge :variant="s.source === SOURCE_SYSTEM ? 'secondary' : 'outline'">
            {{ s.source === SOURCE_SYSTEM ? "系统内置" : "自建" }}
          </StatusBadge>
        </div>

        <!-- Actions: row 2 = 学段 + 改名 (equal width on mobile), row 3 = 删除 (full width) -->
        <div class="flex flex-col gap-2 sm:ml-auto sm:flex-row sm:items-center sm:gap-2">
          <div class="grid grid-cols-2 gap-2 sm:flex sm:gap-2">
            <Button
              variant="outline"
              size="sm"
              icon="visibility"
              class="w-full sm:w-auto"
              @click="askMask(s)"
            >
              学段
            </Button>
            <Button
              variant="outline"
              size="sm"
              icon="edit"
              class="w-full sm:w-auto"
              @click="askRename(s)"
            >
              改名
            </Button>
          </div>
          <!-- Destructive action uses the destructive variant; full-width on mobile
               for an easy touch target, inline on sm+. -->
          <Button
            v-if="s.source !== SOURCE_SYSTEM"
            variant="destructive"
            size="sm"
            icon="delete"
            class="w-full sm:w-auto"
            @click="askDelete(s)"
          >
            删除
          </Button>
        </div>
      </li>
    </ul>

    <PromptModal
      :show="addOpen"
      title="添加科目"
      label="科目名称"
      placeholder="如：科学"
      confirm-label="添加"
      :loading="saving"
      :error="formError"
      @close="addOpen = false"
      @submit="createSubject"
    />

    <PromptModal
      :show="renameOpen"
      title="科目改名"
      label="新名称"
      :model-value="renameTarget?.name ?? ''"
      :loading="saving"
      :error="formError"
      @close="renameOpen = false"
      @submit="renameSubject"
    />

    <SubjectMaskDialog
      :show="maskOpen"
      :subject="maskTarget"
      :loading="savingMask"
      :error="maskError"
      @close="maskOpen = false"
      @submit="saveMask"
    />

    <ConfirmDialog
      :show="deleteOpen"
      title="删除科目"
      :message="deleteMessage"
      confirm-label="删除"
      :loading="deleting"
      @cancel="deleteOpen = false"
      @confirm="confirmDelete"
    />
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import Button from "../ui/Button.vue";
import ConfirmDialog from "../ConfirmDialog.vue";
import EmptyState from "../EmptyState.vue";
import ErrorState from "../ErrorState.vue";
import StatusBadge from "../StatusBadge.vue";
import PromptModal from "./PromptModal.vue";
import SubjectMaskDialog from "./SubjectMaskDialog.vue";
import { settingsApi } from "../../api/settings";
import type { SubjectSummary } from "../../api/types";

/**
 * SubjectsSection — the "subject management" section of the settings page (subjects table).
 *
 * System-preset subjects (source=1) rename only, never delete. No exam-count/theme-color
 * columns (the table lacks them, no aggregate endpoint). `code` stays hidden: a stable
 * seed-dedup identifier, not for parents (still carried in the DTO).
 */
const SOURCE_SYSTEM = 1;

const subjects = ref<SubjectSummary[]>([]);
const loading = ref(false);
const error = ref("");

const addOpen = ref(false);
const renameOpen = ref(false);
const renameTarget = ref<SubjectSummary | null>(null);

const maskOpen = ref(false);
const maskTarget = ref<SubjectSummary | null>(null);
const savingMask = ref(false);
const maskError = ref("");

const deleteOpen = ref(false);
const deleteTarget = ref<SubjectSummary | null>(null);

const saving = ref(false);
const deleting = ref(false);
const formError = ref("");

const deleteMessage = computed(() =>
  deleteTarget.value ? `确认删除科目「${deleteTarget.value.name}」？该操作不可撤销。` : "",
);

async function load(): Promise<void> {
  loading.value = true;
  error.value = "";
  try {
    subjects.value = await settingsApi.subjects();
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    loading.value = false;
  }
}

async function createSubject(name: string): Promise<void> {
  saving.value = true;
  formError.value = "";
  try {
    await settingsApi.createSubject(name);
    addOpen.value = false;
    await load();
  } catch (e) {
    formError.value = e instanceof Error ? e.message : String(e);
  } finally {
    saving.value = false;
  }
}

function askRename(s: SubjectSummary): void {
  renameTarget.value = s;
  formError.value = "";
  renameOpen.value = true;
}

async function renameSubject(name: string): Promise<void> {
  if (!renameTarget.value) return;
  saving.value = true;
  formError.value = "";
  try {
    await settingsApi.renameSubject(renameTarget.value.id, name);
    renameOpen.value = false;
    await load();
  } catch (e) {
    formError.value = e instanceof Error ? e.message : String(e);
  } finally {
    saving.value = false;
  }
}

function askDelete(s: SubjectSummary): void {
  deleteTarget.value = s;
  deleteOpen.value = true;
}

async function confirmDelete(): Promise<void> {
  if (!deleteTarget.value) return;
  deleting.value = true;
  try {
    await settingsApi.deleteSubject(deleteTarget.value.id);
    deleteOpen.value = false;
    await load();
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    deleting.value = false;
  }
}

function askMask(s: SubjectSummary): void {
  maskTarget.value = s;
  maskError.value = "";
  maskOpen.value = true;
}

async function saveMask(mask: number): Promise<void> {
  if (!maskTarget.value) return;
  savingMask.value = true;
  maskError.value = "";
  try {
    await settingsApi.updateSubjectMask(maskTarget.value.id, mask);
    maskOpen.value = false;
    await load();
  } catch (e) {
    maskError.value = e instanceof Error ? e.message : String(e);
  } finally {
    savingMask.value = false;
  }
}

onMounted(load);
</script>
