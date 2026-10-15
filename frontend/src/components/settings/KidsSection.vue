<template>
  <section id="section-kids" class="bg-card text-card-foreground flex flex-col gap-4 rounded-xl border p-6 shadow-sm">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <h2 class="text-base font-semibold tracking-tight">孩子与免密授权</h2>
      <Button variant="outline" icon="person_add" @click="openCreate">
        新登记孩子
      </Button>
    </div>

    <ErrorState v-if="error !== ''" :message="error" retry-label="重试" @retry="load" />
    <div v-else-if="loading" class="space-y-2">
      <div v-for="i in 2" :key="i" class="bg-muted animate-pulse h-12 rounded-lg" />
    </div>
    <EmptyState
      v-else-if="kids.length === 0"
      icon="person"
      title="还没有登记孩子"
      description="登记后即可为孩子上传试卷、查看学情"
      action-label="新登记孩子"
      @action="openCreate"
    />

    <ul v-else class="divide-border/60 divide-y">
      <li v-for="k in kids" :key="k.id" class="flex flex-wrap items-center gap-3 py-3">
        <Icon name="person" :size="18" class="text-muted-foreground shrink-0" />
        <LegendChip
          bare
          :dot-class="statusDotClass(k.status)"
          :title="kidStatusText(k)"
          :aria-label="kidStatusText(k)"
          role="img"
        />
        <span class="font-medium" :class="k.status === KID_STATUS.DELETED && 'text-muted-foreground'">
          {{ k.name }}
        </span>
        <div class="text-muted-foreground flex items-center justify-center gap-4 text-xs"
        >
          <span class="bg-accent inline-flex items-center gap-1 rounded-xl px-2.5 py-1">
            {{ kidStatusText(k) }}
          </span>
        </div>

        <!-- Actions: on mobile a 2x2 grid (two per row, equal width, edges aligned);
             inline right-aligned on sm+. outline variant so the buttons read as a
             consistent control group rather than plain text. -->
        <span class="grid w-full grid-cols-2 gap-2 sm:ml-auto sm:flex sm:w-auto sm:justify-end">
          <!-- Deleted: restore only. -->
          <Button
            v-if="k.status === KID_STATUS.DELETED"
            variant="destructive"
            size="sm"
            class="col-span-2 w-full sm:col-span-1 sm:w-auto"
            icon="refresh"
            @click="restore(k)"
          >
            恢复
          </Button>

          <template v-else>
            <Button
              variant="outline"
              size="sm"
              class="w-full sm:w-auto"
              icon="edit"
              @click="openEdit(k.id)"
            >
              编辑
            </Button>
            <Button
              variant="outline"
              size="sm"
              class="w-full sm:w-auto"
              icon="refresh"
              @click="resetCode(k)"
            >
              重置访问码
            </Button>
            <Button
              variant="outline"
              size="sm"
              class="w-full sm:w-auto"
              icon="visibility_off"
              @click="resetPin(k)"
            >
              重置 PIN
            </Button>
            <!-- Graduated is a terminal state: no delete (and also no restore). -->
            <Button
              v-if="k.status === KID_STATUS.ACTIVE"
              variant="destructive"
              size="sm"
              class="w-full sm:w-auto"
              icon="delete"
              @click="askDelete(k)"
            >
              删除
            </Button>
          </template>
        </span>
      </li>
    </ul>
    <!-- Status-dot legend (same as the exam-list page): green=normal / red=deleted.
         Mobile: persistent accent chips; desktop: transparent until hover. -->
    <div class="text-muted-foreground flex items-center justify-center gap-4 text-xs"
    >
      <span class="hover:bg-accent hover:text-accent-foreground inline-flex items-center gap-1 rounded-xl bg-accent/60 px-2.5 py-1 sm:bg-transparent">
        <span class="bg-success inline-block size-2 rounded-full"></span>正常
      </span>
      <span class="hover:bg-accent hover:text-accent-foreground inline-flex items-center gap-1 rounded-xl bg-accent/60 px-2.5 py-1 sm:bg-transparent">
        <span class="bg-destructive inline-block size-2 rounded-full"></span>删除
      </span>
    </div>

    <!-- Edit: reuse the same form dialog (create is extracted into KidCreateDialog, shared with the top bar / list pages).
         The subject whitelist is edited inside this dialog and saved in the same atomic write. -->
    <KidFormModal
      :show="formOpen"
      mode="edit"
      :initial="editing"
      :saving="saving"
      :error="formError"
      @close="formOpen = false"
      @submit="onSubmitEdit"
    />

    <!-- Register a new kid: shares the same creation code as the top-bar switcher and the student list page. -->
    <KidCreateDialog ref="createDialog" @created="onCreated" />

    <AccessCodeModal
      :show="codeOpen"
      :kid-name="codeKidName"
      :url="codeURL"
      @close="codeOpen = false"
    />

    <PromptModal
      :show="pinOpen"
      title="重置访问 PIN"
      label="新 PIN（4-6 位数字）"
      hint="留空表示清除 PIN，孩子需重新通过邀请链接设置 PIN 后才能登录。"
      confirm-label="保存"
      secret
      :required="false"
      :max-length="6"
      :loading="pinSaving"
      :error="pinError"
      :validate="validatePin"
      @close="pinOpen = false"
      @submit="submitPin"
    />

    <ConfirmDialog
      :show="deleteOpen"
      title="删除孩子"
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
import KidCreateDialog from "../KidCreateDialog.vue";
import KidFormModal, { type KidFormValue } from "./KidFormModal.vue";
import AccessCodeModal from "./AccessCodeModal.vue";
import LegendChip from "../LegendChip.vue";
import PromptModal from "./PromptModal.vue";
import { kidApi } from "../../api/kid";
import { gradeLabelOr } from "../../lib/grade";
import { kidStatusLabel, kidStatusVariant } from "../../lib/kid";
import { KID_STATUS, type KidSummary } from "../../api/types";

/**
 * KidsSection — the "kids & passwordless access" section of the settings page.
 *
 * The list includes deleted (include_deleted=true): active edits/resets/deletes; graduated
 * keeps edit & reset only (terminal — neither deletable nor restorable); deleted offers
 * only restore. Every action goes through a dialog. No hard delete here (future CLI-only).
 */
const kids = ref<KidSummary[]>([]);
const loading = ref(false);
const error = ref("");

const formOpen = ref(false);
const editing = ref<KidFormValue | null>(null);
const editingID = ref(0);
const saving = ref(false);
const formError = ref("");

const createDialog = ref<InstanceType<typeof KidCreateDialog> | null>(null);

const codeOpen = ref(false);
const codeKidName = ref("");
const codeURL = ref("");

const pinOpen = ref(false);
const pinKid = ref<KidSummary | null>(null);
const pinSaving = ref(false);
const pinError = ref("");

const deleteOpen = ref(false);
const deleteTarget = ref<KidSummary | null>(null);
const deleting = ref(false);

const deleteMessage = computed(() =>
  deleteTarget.value ? `确认删除「${deleteTarget.value.name}」？该学生的试卷与分析将不再可见。` : "",
);

async function load(): Promise<void> {
  loading.value = true;
  error.value = "";
  try {
    kids.value = await kidApi.list({ include_deleted: true });
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    loading.value = false;
  }
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

/** Grade when active, status label otherwise. */
function kidStatusText(k: KidSummary): string {
  return k.status === KID_STATUS.ACTIVE ? gradeLabelOr(k.grade) : kidStatusLabel(k.status);
}

/** Register a new kid via the shared dialog. */
function openCreate(): void {
  createDialog.value?.open();
}

async function openEdit(id: number): Promise<void> {
  editingID.value = id;
  formError.value = "";
  try {
    // Load profile + subject whitelist together (saved as one atomic write).
    const [d, subs] = await Promise.all([kidApi.get(id), kidApi.subjects(id)]);
    editing.value = {
      name: d.name,
      grade: d.grade,
      gender: d.gender ?? -1,
      track: d.track,
      subjectIds: subs.map((s) => s.id),
    };
    formOpen.value = true;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  }
}

/** Shared-dialog creation succeeded: refresh the list. */
async function onCreated(): Promise<void> {
  await load();
}

async function onSubmitEdit(payload: {
  name: string;
  grade: number;
  gender: number;
  track: number;
  subjectIds: number[];
}): Promise<void> {
  saving.value = true;
  formError.value = "";
  try {
    await kidApi.update(editingID.value, {
      name: payload.name,
      grade: payload.grade,
      // <0 means unset: omit it, or the DB CHECK rejects it.
      gender: payload.gender < 0 ? undefined : payload.gender,
      track: payload.track,
      subject_ids: payload.subjectIds,
    });
    formOpen.value = false;
    await load();
  } catch (e) {
    formError.value = e instanceof Error ? e.message : String(e);
  } finally {
    saving.value = false;
  }
}

async function resetCode(k: KidSummary): Promise<void> {
  try {
    const res = await kidApi.resetAccessCode(k.id);
    codeKidName.value = k.name;
    codeURL.value = res.kid_url;
    codeOpen.value = true;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  }
}

function resetPin(k: KidSummary): void {
  pinKid.value = k;
  pinError.value = "";
  pinOpen.value = true;
}

function validatePin(v: string): string {
  if (v === "") return "";
  return /^\d{4,6}$/.test(v) ? "" : "PIN 必须是 4-6 位数字";
}

async function submitPin(value: string): Promise<void> {
  if (!pinKid.value || validatePin(value) !== "") return;
  pinSaving.value = true;
  pinError.value = "";
  try {
    await kidApi.resetPin(pinKid.value.id, value === "" ? {} : { new_pin: value });
    pinOpen.value = false;
  } catch (e) {
    pinError.value = e instanceof Error ? e.message : String(e);
  } finally {
    pinSaving.value = false;
  }
}

function askDelete(k: KidSummary): void {
  deleteTarget.value = k;
  deleteOpen.value = true;
}

async function confirmDelete(): Promise<void> {
  if (!deleteTarget.value) return;
  deleting.value = true;
  try {
    await kidApi.remove(deleteTarget.value.id);
    deleteOpen.value = false;
    await load();
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    deleting.value = false;
  }
}

/** Restore a deleted kid (low-risk: no second confirmation). */
async function restore(k: KidSummary): Promise<void> {
  try {
    await kidApi.restore(k.id);
    await load();
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  }
}

onMounted(load);
</script>
