<template>
  <div class="bg-card text-card-foreground flex flex-col gap-4 rounded-xl border p-6 shadow-sm">
    <div class="flex items-center justify-between">
      <h2 class="text-base font-semibold tracking-tight">家长管理</h2>
      <Button variant="outline" icon="person_add" @click="userCreate?.open()">新增家长</Button>
    </div>
    <p class="text-muted-foreground mt-1 text-xs">
      家长账号可多位共用一台 Box；只有自己可以改名 / 改 PIN 或做首次设置。
    </p>

    <ErrorState v-if="error !== ''" :message="error" retry-label="重试" @retry="load" />
    <div v-else-if="loading && users.length === 0" class="bg-muted animate-pulse h-12 rounded-lg" />
    <EmptyState
      v-else-if="users.length === 0"
      icon="person"
      title="还没有家长账号"
      description="登记首位家长（如：妈妈）"
      action-label="新增家长"
      @action="userCreate?.open()"
    />
    <ul v-else class="divide-border/60 mt-2 divide-y">
      <li v-for="u in users" :key="u.id" class="flex flex-wrap items-center gap-3 py-3">
        <Icon name="person" :size="18" class="text-muted-foreground shrink-0" />
        <div class="min-w-0">
          <p class="flex items-center gap-2 font-medium">
            <span class="truncate">{{ u.display_name }}</span>
            <StatusBadge v-if="u.id === me?.id" variant="secondary">本机</StatusBadge>
          </p>
          <p class="text-muted-foreground mt-0.5 text-xs">{{ u.pin_set ? "已设 PIN" : "未设 PIN" }}</p>
        </div>
        <span v-if="u.status !== 1" class="text-muted-foreground text-xs">停用</span>
        <!-- Self actions take their own full-width row on mobile (two equal-width
             outline buttons), inline right on sm+. -->
        <div v-else class="grid w-full grid-cols-2 gap-2 sm:ml-auto sm:flex sm:w-auto sm:shrink-0 sm:items-center">
          <template v-if="u.id === me?.id">
            <Button variant="outline" size="sm" class="w-full sm:w-auto" icon="edit" @click="openRename">改名</Button>
            <Button variant="outline" size="sm" class="w-full sm:w-auto" icon="edit" @click="openPin">
              {{ u.pin_set ? "改 PIN" : "设 PIN" }}
            </Button>
          </template>
          <span v-else class="text-muted-foreground col-span-2 text-xs sm:col-span-1">仅本人可改</span>
        </div>
      </li>
    </ul>

    <UserCreateDialog ref="userCreate" @created="load" />
    <PromptModal
      :show="renameOpen"
      title="修改家长昵称"
      label="昵称"
      :model-value="me?.display_name ?? ''"
      :loading="renameSaving"
      :error="renameError"
      @close="renameOpen = false"
      @submit="rename"
    />
    <PinChangeModal
      :show="pinOpen"
      title="修改入场 PIN"
      hint="PIN 用于家长端免登录进入，仅保存慢哈希。修改成功后沿用新 PIN 无需再登录。"
      :require-old="mePinSet"
      :loading="pinSaving"
      :error="pinError"
      @close="pinOpen = false"
      @submit="changePin"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import Button from "../../components/ui/Button.vue";
import Icon from "../../components/Icon.vue";
import StatusBadge from "../../components/StatusBadge.vue";
import ErrorState from "../../components/ErrorState.vue";
import EmptyState from "../../components/EmptyState.vue";
import UserCreateDialog from "../../components/UserCreateDialog.vue";
import PromptModal from "../../components/settings/PromptModal.vue";
import PinChangeModal from "../../components/settings/PinChangeModal.vue";
import { settingsApi } from "../../api/settings";
import { authApi } from "../../api/auth";
import { writeParentToken } from "../../lib/parentSession";
import type { MeParent, ParentAccount } from "../../api/types";

/**
 * ParentsPage: the "parent management" sub-page of settings (/parents).
 *
 * Only three things: add parent, list parents, and self rename / set / change PIN.
 * No disable/delete/edit-others — the control surface stays locked to creation.
 * The "this device" marker comes from /api/me; first-time PIN likewise self-only
 * (parent B can't reset parent A's PIN).
 */
const me = ref<MeParent | null>(null);
const users = ref<ParentAccount[]>([]);
const mePinSet = computed(() => users.value.find((u) => u.id === me.value?.id)?.pin_set ?? false);

const loading = ref(false);
const error = ref("");

const renameOpen = ref(false);
const renameSaving = ref(false);
const renameError = ref("");

const pinOpen = ref(false);
const pinSaving = ref(false);
const pinError = ref("");

const userCreate = ref<InstanceType<typeof UserCreateDialog> | null>(null);

const renameTarget = computed(() => me.value?.display_name ?? "");

onMounted(load);

async function load(): Promise<void> {
  loading.value = true;
  error.value = "";
  try {
    const [meRes, list] = await Promise.all([authApi.meParent(), settingsApi.users()]);
    me.value = meRes;
    users.value = list;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    loading.value = false;
  }
}

async function rename(value: string): Promise<void> {
  renameSaving.value = true;
  renameError.value = "";
  try {
    // renameParent's return is dropped: load() re-fetches the same data right below.
    await authApi.renameParent(value);
    renameOpen.value = false;
    await load();
  } catch (e) {
    renameError.value = e instanceof Error ? e.message : String(e);
  } finally {
    renameSaving.value = false;
  }
}

function openRename(): void {
  renameError.value = "";
  renameOpen.value = true;
}

function openPin(): void {
  pinError.value = "";
  pinOpen.value = true;
}

async function changePin(oldPin: string, newPin: string): Promise<void> {
  pinSaving.value = true;
  pinError.value = "";
  try {
    const res = await authApi.changePin(mePinSet.value ? oldPin : "", newPin);
    writeParentToken(res.token);
    pinOpen.value = false;
    await load();
  } catch (e) {
    pinError.value = e instanceof Error ? e.message : String(e);
  } finally {
    pinSaving.value = false;
  }
}
</script>