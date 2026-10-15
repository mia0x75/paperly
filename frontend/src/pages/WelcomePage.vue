<template>
  <main class="container mx-auto flex min-h-dvh max-w-xl flex-col justify-center px-4 py-10">
    <div v-if="step === 1" class="bg-card rounded-xl border border-border/50 p-6 shadow-sm">
      <Logo />
      <h1 class="mt-4 text-2xl font-bold tracking-tight">欢迎使用《拾卷》</h1>
      <p class="text-muted-foreground mt-2 text-sm leading-relaxed">
        首次使用，先花 1 分钟完成基础设置：登记家长账号与孩子信息，之后即可上传试卷、追踪学情。
      </p>
      <ul class="text-muted-foreground mt-6 space-y-3 text-sm leading-relaxed">
        <li class="flex items-center gap-3">
          <Icon name="person" :size="20" class="shrink-0" />
          <span>登记家长账号（至少 1 位）</span>
        </li>
        <li class="flex items-center gap-3">
          <Icon name="person_add" :size="20" class="shrink-0" />
          <span>登记孩子信息（至少 1 位）</span>
        </li>
        <li class="flex items-center gap-3">
          <Icon name="check_circle" :size="20" class="shrink-0" />
          <span>完成引导，进入主界面</span>
        </li>
      </ul>
      <Button class="mt-8 w-full" @click="step = 2">开始设置</Button>
    </div>

    <div v-else-if="step === 2" class="bg-card rounded-xl border border-border/50 p-6 shadow-sm">
      <div class="flex items-center justify-between">
        <h1 class="text-base font-semibold tracking-tight">登记家长账号</h1>
        <Button variant="outline" icon="person_add" @click="openUserCreate">添加家长</Button>
      </div>
      <p class="text-muted-foreground mt-1 text-xs">至少 1 位家长；可一次登记多位。</p>

      <ErrorState v-if="userError !== ''" :message="userError" retry-label="重试" @retry="loadUsers" />
      <div v-else-if="usersLoading" class="bg-muted animate-pulse mt-4 h-12 rounded-lg" />
      <EmptyState
        v-else-if="users.length === 0"
        icon="person"
        title="还没有家长账号"
        description="先登记首位家长（如：妈妈）"
        action-label="添加家长"
        @action="openUserCreate"
      />
      <ul v-else class="divide-border/60 mt-4 divide-y">
        <li v-for="u in users" :key="u.id" class="flex items-center gap-3 py-3">
          <Icon name="person" :size="18" class="text-muted-foreground shrink-0" />
          <span class="font-medium">{{ u.display_name }}</span>
          <StatusBadge :variant="u.pin_set ? 'success' : 'outline'">
            {{ u.pin_set ? "已设 PIN" : "未设 PIN" }}
          </StatusBadge>
          <span class="text-muted-foreground ml-auto text-xs">{{ u.status === 1 ? "使用中" : "停用" }}</span>
        </li>
      </ul>

      <div class="mt-8 flex justify-between">
        <Button variant="ghost" @click="step = 1">上一步</Button>
        <Button :disabled="!canNextStep" @click="step = 3">下一步</Button>
      </div>
    </div>

    <div v-else class="bg-card rounded-xl border border-border/50 p-6 shadow-sm">
      <div class="flex items-center justify-between">
        <h1 class="text-base font-semibold tracking-tight">登记孩子</h1>
        <Button variant="outline" icon="person_add" @click="openKidCreate">添加孩子</Button>
      </div>
      <p class="text-muted-foreground mt-1 text-xs">至少 1 位孩子；登记后可上传试卷、查看学情。</p>

      <ErrorState v-if="kidError !== ''" :message="kidError" retry-label="重试" @retry="loadKids" />
      <div v-else-if="kidsLoading" class="bg-muted animate-pulse mt-4 h-12 rounded-lg" />
      <EmptyState
        v-else-if="kids.length === 0"
        icon="person"
        title="还没有孩子"
        description="登记首位孩子（如：小明）"
        action-label="添加孩子"
        @action="openKidCreate"
      />
      <ul v-else class="divide-border/60 mt-4 divide-y">
        <li v-for="k in kids" :key="k.id" class="flex items-center gap-3 py-3">
          <Icon name="person" :size="18" class="text-muted-foreground shrink-0" />
          <span class="font-medium">{{ k.name }}</span>
          <span class="text-muted-foreground text-xs">{{ gradeLabel(k.grade) ?? `年级 ${k.grade}` }}</span>
        </li>
      </ul>

      <div v-if="completingError" class="text-destructive mt-4 text-sm">{{ completingError }}</div>

      <div class="mt-8 flex justify-between">
        <Button variant="ghost" @click="step = 2">上一步</Button>
        <Button :loading="completing" :disabled="!canFinish" @click="finish">
          完成引导
        </Button>
      </div>
    </div>

    <UserCreateDialog ref="userCreate" @created="onUserCreated" />
    <KidCreateDialog ref="kidCreate" @created="onKidCreated" />
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import Logo from "../components/Logo.vue";
import Icon from "../components/Icon.vue";
import Button from "../components/ui/Button.vue";
import StatusBadge from "../components/StatusBadge.vue";
import ErrorState from "../components/ErrorState.vue";
import EmptyState from "../components/EmptyState.vue";
import UserCreateDialog from "../components/UserCreateDialog.vue";
import KidCreateDialog from "../components/KidCreateDialog.vue";
import { settingsApi } from "../api/settings";
import { onboardingApi } from "../api/onboarding";
import { resetOnboardingCache } from "../lib/onboarding";
import { writeParentToken } from "../lib/parentSession";
import { kidApi } from "../api/kid";
import { gradeLabel } from "../lib/grade";
import type { KidSummary, ParentAccount } from "../api/types";

/**
 * First-run wizard (/welcome), rendered without any nav shell.
 * Flow: welcome -> add parents -> add kids -> POST /onboarding/complete -> '/'.
 *
 * No overall progress bar: a one-pass setup needs no progress UI. "Next"/"Finish"
 * stay disabled until an entity exists; the backend re-validates as backstop.
 */
const router = useRouter();

const step = ref(1);
const completing = ref(false);
const completingError = ref("");

const users = ref<ParentAccount[]>([]);
const usersLoading = ref(false);
const userError = ref("");

const kids = ref<KidSummary[]>([]);
const kidsLoading = ref(false);
const kidError = ref("");

const userCreate = ref<InstanceType<typeof UserCreateDialog> | null>(null);
const kidCreate = ref<InstanceType<typeof KidCreateDialog> | null>(null);

const canNextStep = computed(() => users.value.length > 0);
const canFinish = computed(() => kids.value.length > 0);

onMounted(() => {
  void loadUsers();
  void loadKids();
});

async function loadUsers(): Promise<void> {
  usersLoading.value = true;
  userError.value = "";
  try {
    users.value = await settingsApi.users();
  } catch (e) {
    userError.value = e instanceof Error ? e.message : String(e);
  } finally {
    usersLoading.value = false;
  }
}

async function loadKids(): Promise<void> {
  kidsLoading.value = true;
  kidError.value = "";
  try {
    kids.value = await kidApi.list();
  } catch (e) {
    kidError.value = e instanceof Error ? e.message : String(e);
  } finally {
    kidsLoading.value = false;
  }
}

function openUserCreate(): void {
  userCreate.value?.open();
}

function openKidCreate(): void {
  kidCreate.value?.open();
}

function onUserCreated(): void {
  void loadUsers();
}

function onKidCreated(): void {
  void loadKids();
}

async function finish(): Promise<void> {
  completing.value = true;
  completingError.value = "";
  try {
    const res = await onboardingApi.complete();
    // Complete may also issue a default parent JWT; store it so the home guard passes.
    if (res.token) {
      writeParentToken(res.token);
    }
    // Reset the guard cache to refetch fresh state before home (it only caches true).
    resetOnboardingCache();
    await router.replace("/");
  } catch (e) {
    completingError.value = e instanceof Error ? e.message : String(e);
  } finally {
    completing.value = false;
  }
}
</script>
