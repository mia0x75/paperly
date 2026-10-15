<template>
  <main class="container mx-auto flex min-h-dvh max-w-md flex-col justify-center px-4 py-10">
    <ErrorState
      v-if="accountsError !== ''"
      :message="accountsError"
      retry-label="重试"
      @retry="loadAccounts"
    />
    <div v-else class="bg-card rounded-xl border border-border/50 p-6 shadow-sm">
      <Logo />
      <h1 class="mt-4 text-2xl font-bold tracking-tight">家长登录</h1>
      <p class="text-muted-foreground mt-2 text-sm leading-relaxed">选择账号，输入入场密码，开始《拾卷》。</p>

      <div v-if="accountsLoading" class="bg-muted animate-pulse mt-6 h-11 rounded-lg" />

      <form v-else class="mt-6 space-y-4" @submit.prevent="submit">
        <div class="space-y-1.5">
          <label class="text-sm font-medium" for="login-account">家长账号</label>
          <!--
            Native <select> + optgroup grouping: accounts with a PIN set can log in
            directly; those without one are grayed out as a whole group (disabled
            options cannot be selected), with a hint.
          -->
          <select
            id="login-account"
            v-model="pickedId"
            :disabled="accounts.length === 0"
            class="bg-background focus-visible:ring-ring h-11 w-full rounded-md border px-3 text-sm outline-none focus-visible:ring-2 disabled:cursor-not-allowed"
          >
            <option v-if="accounts.length === 0" value="0" disabled>暂无可登录账号</option>
            <optgroup label="已设 PIN">
              <option v-for="u in pinSetAccounts" :key="u.id" :value="u.id">{{ u.display_name }}</option>
            </optgroup>
            <optgroup v-if="noPinAccounts.length > 0" label="未设 PIN（暂不可登录）">
              <option v-for="u in noPinAccounts" :key="u.id" :value="u.id" disabled>{{ u.display_name }}</option>
            </optgroup>
          </select>
          <p v-if="noPinAccounts.length > 0" class="text-muted-foreground text-xs">
            未设 PIN 的账号需先在"家长管理"页设置 PIN 后才能登录。
          </p>
        </div>

        <div class="space-y-1.5">
          <label class="text-sm font-medium" for="login-pin">入场 PIN</label>
          <input
            id="login-pin"
            :value="pin"
            type="password"
            inputmode="numeric"
            class="bg-background focus-visible:ring-ring h-11 w-full rounded-md border px-3 text-sm outline-none focus-visible:ring-2"
            placeholder="4-6 位数字，登记时设置"
            :maxlength="6"
            autocomplete="current-password"
            @input="onPinInput"
          />
        </div>

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
import { useRoute, useRouter } from "vue-router";
import Logo from "../components/Logo.vue";
import Button from "../components/ui/Button.vue";
import ErrorState from "../components/ErrorState.vue";
import { authApi } from "../api/auth";
import { ApiError } from "../api/client";
import { writeParentToken } from "../lib/parentSession";
import type { AccountSummary } from "../api/types";

/**
 * LoginPage: parent login page (/login).
 *
 * No nav shell; before login there should be no family-side page at all. Account list
 * from anonymous GET /api/auth/parent-accounts (active accounts only, with pin_set):
 * PIN-set accounts selectable, others grayed with a hint. Must not use GET /api/users —
 * it needs a parent token, unavailable before login.
 *
 * Submit POST /api/auth/login (anonymous entry). Business 40100 (e.g. wrong PIN)
 * surfaces the backend message (never trigger "clear session and redirect" — login
 * is anonymous). On success write the token and bounce back to the redirect target.
 */
const route = useRoute();
const router = useRouter();

const accounts = ref<AccountSummary[]>([]);
const accountsLoading = ref(false);
const accountsError = ref("");

const pickedId = ref<number>(0);
const pin = ref("");

const submitting = ref(false);
const loginError = ref("");

const pinSetAccounts = computed(() => accounts.value.filter((u) => u.pin_set));
const noPinAccounts = computed(() => accounts.value.filter((u) => !u.pin_set));

const canSubmit = computed(() => pickedId.value > 0 && pin.value.length >= 4);

onMounted(() => {
  void loadAccounts();
});

async function loadAccounts(): Promise<void> {
  accountsLoading.value = true;
  accountsError.value = "";
  try {
    accounts.value = await authApi.accounts();
  } catch (e) {
    accountsError.value = e instanceof Error ? e.message : String(e);
  } finally {
    accountsLoading.value = false;
  }
}

function onPinInput(event: Event): void {
  loginError.value = "";
  // Controlled input: use :value + a hand-written @input to avoid a race between
  // v-model and the sanitizing handler (both bind @input, and the later-registered
  // v-model would write the dirty value back to the ref). Consolidate here once:
  // strip non-digits on each keystroke and write back via :value, guaranteeing the
  // submitted value is always a digit string.
  const raw = (event.target as HTMLInputElement).value;
  pin.value = raw.replace(/\D/g, "");
}

async function submit(): Promise<void> {
  if (!canSubmit.value || submitting.value) return;
  loginError.value = "";
  submitting.value = true;
  try {
    const res = await authApi.login(pickedId.value, pin.value);
    writeParentToken(res.token);
    await router.replace(redirectTarget());
  } catch (e) {
    loginError.value = e instanceof ApiError || e instanceof Error ? e.message : String(e);
  } finally {
    submitting.value = false;
  }
}

/** Redirect target: trust only same-site paths (starting with / and not // protocol-relative); otherwise home. */
function redirectTarget(): string {
  const r = route.query.redirect;
  if (typeof r === "string" && r.startsWith("/") && !r.startsWith("//")) {
    return r;
  }
  return "/";
}
</script>
