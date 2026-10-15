<template>
  <Modal :show="show" :title="title" @close="close">
    <p v-if="hint" class="text-muted-foreground mt-0 mb-3 text-sm">{{ hint }}</p>

    <label class="flex flex-col gap-1 text-sm">
      <span class="text-muted-foreground">
        {{ label }}<span v-if="required" class="text-destructive">*</span>
      </span>
      <input
        ref="inputEl"
        v-model="value"
        :type="secret ? 'password' : 'text'"
        :inputmode="secret ? 'numeric' : 'text'"
        :maxlength="maxLength"
        :class="[FIELD_BASE, 'h-9']"
        :placeholder="placeholder"
        @keyup.enter="submit"
      />
    </label>

    <p v-if="error" class="text-destructive mt-2 text-sm">{{ error }}</p>

    <template #footer>
      <Button variant="outline" :loading="loading" :disabled="!canSubmit" class="w-full sm:w-auto" @click="submit">{{ confirmLabel }}</Button>
      <Button variant="outline" :disabled="loading" class="w-full sm:w-auto" @click="close">取消</Button>
    </template>
  </Modal>
</template>

<script setup lang="ts">
import { nextTick, ref, watch } from "vue";
import Button from "../ui/Button.vue";
import Modal from "../Modal.vue";
import { FIELD_BASE } from "../../lib/ui";

/**
 * PromptModal — single-field text dialog.
 *
 * Every settings add/rename is "one input + one submit"; one parameterized dialog
 * beats four near-identical forms. Used in place of navigation: renaming must not
 * throw a parent off the settings page.
 */
const props = withDefaults(
  defineProps<{
    show: boolean;
    title: string;
    label: string;
    /** Initial value (overwrites stale input on open). */
    modelValue?: string;
    hint?: string;
    placeholder?: string;
    confirmLabel?: string;
    loading?: boolean;
    /** Validation-failure copy, supplied by the caller. */
    error?: string;
    required?: boolean;
    secret?: boolean;
    maxLength?: number;
    /** Extra validation; non-empty return = failure. */
    validate?: (value: string) => string;
  }>(),
  { confirmLabel: "保存", required: true },
);

const emit = defineEmits<{ close: []; submit: [string] }>();

const value = ref(props.modelValue ?? "");
const inputEl = ref<HTMLInputElement | null>(null);

// Reset from initial on every open (never show the previous input).
watch(
  () => props.show,
  async (show) => {
    if (!show) return;
    value.value = props.modelValue ?? "";
    await nextTick();
    inputEl.value?.focus();
  },
);

const canSubmit = (): boolean =>
  !props.loading && (props.required ? value.value.trim().length > 0 : true);

function close(): void {
  emit("close");
}

function submit(): void {
  if (!canSubmit()) return;
  const err = props.validate?.(value.value.trim());
  if (err) return;
  emit("submit", value.value.trim());
}
</script>
