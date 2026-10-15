<template>
  <section id="section-knowledge" class="bg-card text-card-foreground flex flex-col gap-4 rounded-xl border p-6 shadow-sm">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h2 class="text-base font-semibold tracking-tight">知识点管理</h2>
      <!-- Mobile: subject dropdown + add button share one equal-width row; desktop: inline. -->
      <div class="grid w-full grid-cols-2 gap-2 sm:flex sm:w-auto sm:items-center">
        <Select
          v-model="subjectId"
          :options="subjectOptions"
          placeholder="选择科目"
          class="w-full sm:w-40"
          @change="loadKnowledge"
        />
        <Button
          variant="outline"
          class="w-full sm:w-auto"
          icon="add"
          :disabled="subjectId <= 0"
          @click="addOpen = true"
        >
          添加知识点
        </Button>
      </div>
    </div>

    <ErrorState v-if="error !== ''" :message="error" retry-label="重试" @retry="reload" />
    <div v-else-if="loading" class="space-y-2">
      <div v-for="i in 4" :key="i" class="bg-muted animate-pulse h-10 rounded-lg" />
    </div>
    <EmptyState
      v-else-if="subjectId <= 0"
      icon="folder"
      title="请先选择科目"
      description="知识点归属于科目，选择后即可维护"
    />
    <EmptyState
      v-else-if="points.length === 0"
      icon="description"
      title="该科目还没有知识点"
      action-label="添加知识点"
      @action="addOpen = true"
    />

    <ul v-else class="divide-border/60 divide-y">
      <li v-for="kp in points" :key="kp.id" class="flex items-center gap-3 py-2.5">
        <Icon name="description" :size="16" class="text-muted-foreground shrink-0" />
        <span>{{ kp.name }}</span>
        <span class="ml-auto flex items-center gap-2">
          <Button variant="ghost" size="sm" icon="edit" @click="askRename(kp)">编辑</Button>
          <Button variant="ghost" size="sm" icon="delete" @click="askDelete(kp)">删除</Button>
        </span>
      </li>
    </ul>

    <PromptModal
      :show="addOpen"
      title="添加知识点"
      label="知识点名称"
      :loading="saving"
      :error="formError"
      @close="addOpen = false"
      @submit="createPoint"
    />

    <PromptModal
      :show="renameOpen"
      title="编辑知识点"
      label="新名称"
      :model-value="renameTarget?.name ?? ''"
      :loading="saving"
      :error="formError"
      @close="renameOpen = false"
      @submit="renamePoint"
    />

    <ConfirmDialog
      :show="deleteOpen"
      title="删除知识点"
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
import Icon from "../Icon.vue";
import Select from "../ui/Select.vue";
import PromptModal from "./PromptModal.vue";
import { settingsApi } from "../../api/settings";
import type { KnowledgePointSummary, SubjectSummary } from "../../api/types";
import type { SelectOption } from "../../lib/ui";

/**
 * KnowledgeSection — the "knowledge point management" section of the settings page.
 *
 * Points are narrowed by subject first. Listed flat: knowledge_points has no
 * parent/group column, so the design's "group tree" can't be built here.
 */
const subjects = ref<SubjectSummary[]>([]);
const subjectId = ref(0);
const points = ref<KnowledgePointSummary[]>([]);
const loading = ref(false);
const error = ref("");

const addOpen = ref(false);
const renameOpen = ref(false);
const renameTarget = ref<KnowledgePointSummary | null>(null);
const deleteOpen = ref(false);
const deleteTarget = ref<KnowledgePointSummary | null>(null);

const saving = ref(false);
const deleting = ref(false);
const formError = ref("");

const subjectOptions = computed<readonly SelectOption<number>[]>(() =>
  subjects.value.map((s) => ({ value: s.id, label: s.name })),
);

const deleteMessage = computed(() =>
  deleteTarget.value ? `确认删除知识点「${deleteTarget.value.name}」？` : "",
);

async function reload(): Promise<void> {
  error.value = "";
  try {
    subjects.value = await settingsApi.subjects();
    if (subjectId.value <= 0 && subjects.value.length > 0) {
      subjectId.value = subjects.value[0]?.id ?? 0;
    }
    if (subjectId.value > 0) await loadKnowledge();
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  }
}

async function loadKnowledge(): Promise<void> {
  if (subjectId.value <= 0) {
    points.value = [];
    return;
  }
  loading.value = true;
  error.value = "";
  try {
    points.value = await settingsApi.knowledgePoints(subjectId.value);
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    loading.value = false;
  }
}

async function createPoint(name: string): Promise<void> {
  saving.value = true;
  formError.value = "";
  try {
    await settingsApi.createKnowledgePoint(subjectId.value, name);
    addOpen.value = false;
    await loadKnowledge();
  } catch (e) {
    formError.value = e instanceof Error ? e.message : String(e);
  } finally {
    saving.value = false;
  }
}

function askRename(kp: KnowledgePointSummary): void {
  renameTarget.value = kp;
  formError.value = "";
  renameOpen.value = true;
}

async function renamePoint(name: string): Promise<void> {
  if (!renameTarget.value) return;
  saving.value = true;
  formError.value = "";
  try {
    await settingsApi.renameKnowledgePoint(subjectId.value, renameTarget.value.id, name);
    renameOpen.value = false;
    await loadKnowledge();
  } catch (e) {
    formError.value = e instanceof Error ? e.message : String(e);
  } finally {
    saving.value = false;
  }
}

function askDelete(kp: KnowledgePointSummary): void {
  deleteTarget.value = kp;
  deleteOpen.value = true;
}

async function confirmDelete(): Promise<void> {
  if (!deleteTarget.value) return;
  deleting.value = true;
  try {
    await settingsApi.deleteKnowledgePoint(subjectId.value, deleteTarget.value.id);
    deleteOpen.value = false;
    await loadKnowledge();
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    deleting.value = false;
  }
}

onMounted(reload);
</script>
