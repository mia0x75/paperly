<template>
  <UserFormModal
    :show="formOpen"
    mode="create"
    :saving="saving"
    :error="error"
    @close="formOpen = false"
    @submit="submit"
  />
</template>

<script setup lang="ts">
import { ref } from "vue";
import UserFormModal from "./settings/UserFormModal.vue";
import { settingsApi } from "../api/settings";
import type { ParentAccount } from "../api/types";

/**
 * UserCreateDialog -- shared dialog for the parent-facing "register parent".
 *
 * Shared by the onboarding wizard and the settings page: settingsApi.createUser ->
 * emit created for the caller to refresh. Triggered via defineExpose open().
 * Parents have no access_code (the kid's passwordless credential), so no second
 * display dialog after success.
 */
const emit = defineEmits<{ created: [user: ParentAccount] }>();

const formOpen = ref(false);
const saving = ref(false);
const error = ref("");

/** Open the form (UserFormModal resets initial values on show). */
function open(): void {
  error.value = "";
  formOpen.value = true;
}

async function submit(payload: { display_name: string; pin?: string }): Promise<void> {
  saving.value = true;
  error.value = "";
  try {
    const created = await settingsApi.createUser(payload.display_name, payload.pin);
    formOpen.value = false;
    emit("created", created);
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    saving.value = false;
  }
}

defineExpose({ open });
</script>