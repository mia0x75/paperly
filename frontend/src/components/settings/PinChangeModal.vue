<template>
  <Modal :show="show" :title="title" @close="close">
    <p v-if="hint" class="text-muted-foreground mt-0 mb-3 text-sm">{{ hint }}</p>

    <div class="flex flex-col gap-3">
      <label v-if="requireOld" class="flex flex-col gap-1 text-sm">
        <span class="text-muted-foreground">当前 PIN<span class="text-destructive">*</span></span>
        <input
          ref="oldInput"
          v-model="oldPin"
          type="password"
          inputmode="numeric"
          class="bg-background focus-visible:ring-ring h-9 rounded-md border px-3 text-sm outline-none focus-visible:ring-2"
          :maxlength="6"
          placeholder="输入当前 PIN"
          @input="sanitize('oldPin')"
          @keyup.enter="submit"
        />
      </label>

      <label class="flex flex-col gap-1 text-sm">
        <span class="text-muted-foreground">
          新 PIN<span class="text-destructive">*</span>
        </span>
        <input
          ref="newInput"
          v-model="newPin"
          type="password"
          inputmode="numeric"
          class="bg-background focus-visible:ring-ring h-9 rounded-md border px-3 text-sm outline-none focus-visible:ring-2"
          :maxlength="6"
          placeholder="4-6 位数字"
          @input="sanitize('newPin')"
          @keyup.enter="submit"
        />
      </label>
    </div>

    <p v-if="error" class="text-destructive mt-2 text-sm">{{ error }}</p>

    <template #footer>
      <Button :loading="loading" :disabled="!canSubmit" class="w-full sm:w-auto" @click="submit">保存</Button>
      <Button variant="outline" :disabled="loading" class="w-full sm:w-auto" @click="close">取消</Button>
    </template>
  </Modal>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from "vue";
import Button from "../ui/Button.vue";
import Modal from "../Modal.vue";

/**
 * PinChangeModal — two-field change-PIN dialog.
 *
 * PIN change doesn't fit PromptModal's single-field model (verify-old + set-new),
 * but keeps its style (focus on open, non-digit dismiss, Enter submits, read-only
 * while loading). With requireOld=false the old-PIN field hides and the caller
 * passes empty oldPin; submit payload is {oldPin, newPin}.
 */
const props = withDefaults(
  defineProps<{
    show: boolean;
    title: string;
    hint?: string;
    /** First-time setup (no old PIN): hides the current-PIN input. */
    requireOld?: boolean;
    loading?: boolean;
    error?: string;
  }>(),
  { requireOld: true },
);

const emit = defineEmits<{ close: []; submit: [oldPin: string, newPin: string] }>();

const oldPin = ref("");
const newPin = ref("");
const oldInput = ref<HTMLInputElement | null>(null);
const newInput = ref<HTMLInputElement | null>(null);

// Reset inputs on every open (never show a stale PIN).
watch(
  () => props.show,
  async (show) => {
    if (!show) return;
    oldPin.value = "";
    newPin.value = "";
    await nextTick();
    (props.requireOld ? oldInput.value : newInput.value)?.focus();
  },
);

const canSubmit = computed(
  () => !props.loading && /^\d{4,6}$/.test(newPin.value) && (!props.requireOld || /^\d{4,6}$/.test(oldPin.value)),
);

function sanitize(field: "oldPin" | "newPin"): void {
  if (field === "oldPin") {
    oldPin.value = oldPin.value.replace(/\D/g, "");
  } else {
    newPin.value = newPin.value.replace(/\D/g, "");
  }
}

function close(): void {
  emit("close");
}

function submit(): void {
  if (!canSubmit.value) return;
  emit("submit", oldPin.value, newPin.value);
}
</script>