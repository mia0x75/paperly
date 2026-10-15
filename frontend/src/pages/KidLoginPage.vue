<template>
  <main class="container mx-auto flex min-h-dvh max-w-md flex-col justify-center px-4 py-10">
    <ErrorState
      v-if="listError !== ''"
      :message="listError"
      retry-label="重试"
      @retry="loadKids"
    />
    <div v-else class="bg-card rounded-xl border border-border/50 p-6 shadow-sm">
      <Logo />
      <h1 class="mt-4 text-2xl font-bold tracking-tight">孩子登录</h1>
      <p class="text-muted-foreground mt-2 text-sm leading-relaxed">选择你的名字，输入 PIN 进入 Paperly。</p>

      <div v-if="loading" class="bg-muted animate-pulse mt-6 h-11 rounded-lg" />

      <!-- Empty list: actionable hint, no doomed requests. -->
      <p v-else-if="kids.length === 0" class="text-muted-foreground mt-6 text-sm leading-relaxed">
        暂无孩子账号，请家长先邀请。
      </p>

      <form v-else class="mt-6 space-y-4" @submit.prevent="submit">
        <div class="space-y-1.5">
          <label class="text-sm font-medium" for="kid-login-select">孩子</label>
          <select
            id="kid-login-select"
            v-model="pickedId"
            class="bg-background focus-visible:ring-ring h-11 w-full rounded-md border px-3 text-sm outline-none focus-visible:ring-2"
          >
            <option :value="0" disabled>选择孩子</option>
            <option v-for="k in kids" :key="k.id" :value="k.id">{{ k.name }}</option>
          </select>
        </div>

        <div class="space-y-1.5">
          <label class="text-sm font-medium" for="kid-login-pin">PIN</label>
          <input
            id="kid-login-pin"
            :value="pin"
            type="password"
            inputmode="numeric"
            class="bg-background focus-visible:ring-ring h-11 w-full rounded-md border px-3 text-sm outline-none focus-visible:ring-2"
            placeholder="4-6 位数字"
            :maxlength="6"
            autocomplete="current-password"
            @input="onPinInput"
          />
        </div>

        <!--
          Error text is grouped by business code:
            40100 (HTTP 401) = wrong PIN / non-loggable account, shown as one "PIN error" hint
            without distinguishing account details;
            42900 (HTTP 429) = attempt limit reached and locked.
          Both are credential/rate-limit cases rendered here; the session stays
          untouched (client.ts exempts this path).
        -->
        <p v-if="loginError !== ''" class="text-destructive text-sm" role="alert">{{ loginError }}</p>

        <Button type="submit" class="w-full" :loading="submitting" :disabled="!canSubmit">
          登录
        </Button>
      </form>
    </div>
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import Logo from "../components/Logo.vue";
import Button from "../components/ui/Button.vue";
import ErrorState from "../components/ErrorState.vue";
import { authApi } from "../api/auth";
import { ApiError } from "../api/client";
import { writeKidToken } from "../lib/kidSession";
import type { KidAccount } from "../api/types";

/**
 * Kid login (/kid/login) without a nav shell: the landing spot for session-less kid
 * devices (kid guard sends them here). Account list from anonymous GET
 * /api/auth/kid-accounts; submit POST /api/kid/auth/login with user_id + pin.
 *
 * On success write paperly.kid_token then go to /kid; on failure map business codes
 * (40100 PIN error, 42900 too many attempts) without writing a token. Empty list
 * shows "ask a parent" — no accounts means no submit button.
 */
const router = useRouter();

const kids = ref<KidAccount[]>([]);
const loading = ref(true);
const listError = ref("");

const pickedId = ref<number>(0);
const pin = ref("");

const submitting = ref(false);
const loginError = ref("");

const canSubmit = computed(() => pickedId.value > 0 && pin.value.length >= 4 && !submitting.value);

onMounted(() => {
  void loadKids();
});

async function loadKids(): Promise<void> {
  loading.value = true;
  listError.value = "";
  try {
    kids.value = await authApi.kidAccounts();
  } catch (e) {
    listError.value = e instanceof Error ? e.message : String(e);
  } finally {
    loading.value = false;
  }
}

/** Strip non-digits (same as parent login); submitted value is always a digit string. */
function onPinInput(event: Event): void {
  loginError.value = "";
  const raw = (event.target as HTMLInputElement).value;
  pin.value = raw.replace(/\D/g, "").slice(0, 6);
}

/** Failure copy keyed by business code; others pass through the backend message. */
function loginErrorText(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 40100) return "PIN 错误，请重试";
    if (e.code === 42900) return "尝试过多，请稍后";
  }
  return e instanceof Error ? e.message : String(e);
}

async function submit(): Promise<void> {
  if (!canSubmit.value) return;
  loginError.value = "";
  submitting.value = true;
  try {
    const res = await authApi.kidLogin(pickedId.value, pin.value);
    writeKidToken(res.token);
    await router.replace("/kid");
  } catch (e) {
    loginError.value = loginErrorText(e);
  } finally {
    submitting.value = false;
  }
}
</script>
