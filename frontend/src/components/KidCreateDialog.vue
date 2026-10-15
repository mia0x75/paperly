<template>
  <KidFormModal
    :show="formOpen"
    mode="create"
    :saving="saving"
    :error="error"
    @close="formOpen = false"
    @submit="submit"
  />
  <AccessCodeModal
    :show="codeOpen"
    :kid-name="createdName"
    :url="createdURL"
    @close="codeOpen = false"
  />
</template>

<script setup lang="ts">
import { ref } from "vue";
import KidFormModal from "./settings/KidFormModal.vue";
import AccessCodeModal from "./settings/AccessCodeModal.vue";
import { kidApi } from "../api/kid";
import type { KidCreated } from "../api/types";

/**
 * KidCreateDialog -- shared dialog for the parent-facing "register new kid".
 *
 * Shared by the student list, welcome wizard and settings page: kidApi.create ->
 * one-time access link (AccessCodeModal) -> emit created. Triggered via
 * defineExpose open().
 */
const emit = defineEmits<{ created: [kid: KidCreated] }>();

const formOpen = ref(false);
const saving = ref(false);
const error = ref("");
const codeOpen = ref(false);
const createdName = ref("");
const createdURL = ref("");

/** Open the form (KidFormModal resets initial values on show). */
function open(): void {
  error.value = "";
  formOpen.value = true;
}

async function submit(payload: {
  name: string;
  grade: number;
  gender: number;
  subjectIds: number[];
  pin?: string;
}): Promise<void> {
  saving.value = true;
  error.value = "";
  try {
    const created = await kidApi.create({
      name: payload.name,
      grade: payload.grade,
      // <0 means unset: omit it, or DB CHECK(gender IN (0,1)) rejects it.
      gender: payload.gender < 0 ? undefined : payload.gender,
      pin: payload.pin === "" ? undefined : payload.pin,
      subject_ids: payload.subjectIds,
    });
    formOpen.value = false;
    createdName.value = created.name;
    createdURL.value = `${location.origin}${created.kid_url}`;
    codeOpen.value = true;
    emit("created", created);
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    saving.value = false;
  }
}

defineExpose({ open });
</script>
