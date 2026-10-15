<template>
  <main class="container mx-auto flex min-h-dvh max-w-md flex-col justify-center px-4 py-10">
    <ErrorState
      v-if="verifyError !== ''"
      :message="verifyError"
      retry-label="重试"
      @retry="verify"
    />
    <div v-else class="bg-card rounded-xl border border-border/50 p-6 shadow-sm">
      <Logo />

      <!-- Checking: skeleton matches the title+form shape, no blank window. -->
      <template v-if="checking">
        <div class="bg-muted animate-pulse mt-4 h-8 w-2/3 rounded-md" />
        <div class="bg-muted animate-pulse mt-6 h-11 rounded-lg" />
        <div class="bg-muted animate-pulse mt-3 h-11 rounded-lg" />
      </template>

      <template v-else-if="!codeValid">
        <h1 class="mt-4 text-2xl font-bold tracking-tight">邀请码无效</h1>
        <p class="text-muted-foreground mt-2 text-sm leading-relaxed">
          该邀请链接已失效，请向家长重新获取邀请链接后再试。
        </p>
      </template>

      <template v-else>
        <h1 class="mt-4 text-2xl font-bold tracking-tight">
          欢迎，{{ kidName }}。请设置你的 PIN
        </h1>
        <p class="text-muted-foreground mt-2 text-sm leading-relaxed">
          PIN 为 4-6 位数字，设置后即可从孩子登录页进入。
        </p>

        <form class="mt-6 space-y-4" @submit.prevent="submit">
          <div class="space-y-1.5">
            <label class="text-sm font-medium" for="invite-pin">PIN</label>
            <input
              id="invite-pin"
              :value="pin"
              type="password"
              inputmode="numeric"
              class="bg-background focus-visible:ring-ring h-11 w-full rounded-md border px-3 text-sm outline-none focus-visible:ring-2"
              placeholder="4-6 位数字"
              :maxlength="6"
              autocomplete="new-password"
              @input="onPinInput($event, 'pin')"
            />
            <p class="text-muted-foreground text-xs">4-6 位数字</p>
          </div>

          <div class="space-y-1.5">
            <label class="text-sm font-medium" for="invite-confirm">确认 PIN</label>
            <input
              id="invite-confirm"
              :value="confirm"
              type="password"
              inputmode="numeric"
              class="bg-background focus-visible:ring-ring h-11 w-full rounded-md border px-3 text-sm outline-none focus-visible:ring-2"
              placeholder="再输入一次"
              :maxlength="6"
              autocomplete="new-password"
              @input="onPinInput($event, 'confirm')"
            />
            <!-- Immediate confirm feedback: tell the user the two entries differ without waiting for submit. -->
            <p v-if="confirmMismatch" class="text-destructive text-xs">两次输入的 PIN 不一致</p>
          </div>

          <p v-if="formError !== ''" class="text-destructive text-sm" role="alert">{{ formError }}</p>

          <Button type="submit" class="w-full" :loading="submitting" :disabled="!canSubmit">
            进入学习空间
          </Button>
        </form>
      </template>
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
import { writeKidToken } from "../lib/kidSession";

/**
 * InvitePage: invite setup page (/invite/:code).
 *
 * No nav shell: the kid's first landing spot, no navigation before login. Link comes
 * from the parent (kid_url = /invite/<access_code>).
 *
 * Flow:
 *   1. Open: POST /api/kid/invite/verify {code} — invalid returns 200 + {valid:false}
 *      (backend doesn't distinguish causes; one message shown);
 *   2. Valid -> "Welcome, {name}, set your PIN" form (4-6 digits + confirmation);
 *   3. Submit POST /api/kid/invite/setup {code, pin} — returns a kid JWT directly;
 *      write paperly.kid_token, go to /kid.
 *
 * Failures: verify network/5xx -> ErrorState + retry; setup failure -> red text +
 * resubmit (42900 copy from backend).
 */
const route = useRoute();
const router = useRouter();

/** Invite code from route params; missing counts as invalid. */
const code = computed(() => String(route.params.code ?? ""));

const checking = ref(true);
const codeValid = ref(false);
const kidName = ref("");
const verifyError = ref("");

const pin = ref("");
const confirm = ref("");
const submitting = ref(false);
const formError = ref("");

/** Submit needs both entries filled; short-PIN errors show on submit, not via disabling. */
const canSubmit = computed(() => pin.value !== "" && confirm.value !== "" && !submitting.value);

/** Mismatch hint when the confirm box is non-empty and differs. */
const confirmMismatch = computed(
  () => confirm.value !== "" && pin.value !== "" && confirm.value !== pin.value,
);

onMounted(() => {
  void verify();
});

async function verify(): Promise<void> {
  checking.value = true;
  verifyError.value = "";
  try {
    const res = await authApi.inviteVerify(code.value);
    codeValid.value = res.valid;
    kidName.value = res.valid ? (res.name ?? "") : "";
  } catch (e) {
    // Network/5xx: actionable error + retry, never a blank page.
    codeValid.value = false;
    verifyError.value = e instanceof Error ? e.message : String(e);
  } finally {
    checking.value = false;
  }
}

/** Controlled input: strip non-digits (same as LoginPage); submitted value is a digit string. */
function onPinInput(event: Event, field: "pin" | "confirm"): void {
  formError.value = "";
  const raw = (event.target as HTMLInputElement).value;
  const next = raw.replace(/\D/g, "").slice(0, 6);
  if (field === "pin") pin.value = next;
  else confirm.value = next;
}

async function submit(): Promise<void> {
  if (!canSubmit.value) return;
  formError.value = "";

  if (!/^\d{4,6}$/.test(pin.value)) {
    formError.value = "PIN 须为 4-6 位数字";
    return;
  }
  if (pin.value !== confirm.value) {
    formError.value = "两次输入的 PIN 不一致";
    return;
  }

  submitting.value = true;
  try {
    const res = await authApi.inviteSetup(code.value, pin.value);
    writeKidToken(res.token);
    await router.replace("/kid");
  } catch (e) {
    // 400/429 copy comes from the backend message; retry allowed.
    formError.value = e instanceof Error ? e.message : String(e);
  } finally {
    submitting.value = false;
  }
}
</script>
