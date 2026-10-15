<template>
  <Modal :show="show" :title="mode === 'create' ? '登记家长' : '编辑家长'" @close="close">
    <div class="space-y-4">
      <label class="flex flex-col gap-1">
        <span class="text-muted-foreground">
          昵称<span class="text-destructive">*</span>
        </span>
        <input v-model="form.display_name" type="text" :class="[FIELD_BASE, 'h-9']" placeholder="如：妈妈、爸爸" />
      </label>

      <label class="flex flex-col gap-1">
        <span class="text-muted-foreground">入场 PIN（4-6 位数字）</span>
        <input v-model="form.pin" type="text" :class="[FIELD_BASE, 'h-9']" placeholder="4-6 位数字" />
      </label>

      <p v-if="error" class="text-destructive m-0 text-sm">{{ error }}</p>
    </div>

    <template #footer>
      <Button :loading="saving" :disabled="!canSubmit" class="w-full sm:w-auto" @click="submit">
        {{ mode === "create" ? "登记" : "保存" }}
      </Button>
      <Button variant="outline" :disabled="saving" class="w-full sm:w-auto" @click="close">取消</Button>
    </template>
  </Modal>
</template>

<script setup lang="ts">
import { reactive, watch } from "vue";
import Button from "../ui/Button.vue";
import Modal from "../Modal.vue";
import { FIELD_BASE } from "../../lib/ui";

/** Initial value (from the list row when editing). */
export interface UserFormValue {
  display_name: string;
}

/**
 * UserFormModal — dialog for registering / editing a parent account.
 *
 * Same shape as KidFormModal: add/edit always goes through a dialog, not a page.
 * Optional PIN at creation (4-6 digits, same rules as kids); backend stores only a hash.
 */
const props = withDefaults(
  defineProps<{
    show: boolean;
    mode: "create" | "edit";
    initial?: UserFormValue | null;
    saving?: boolean;
    error?: string;
  }>(),
  { saving: false, error: "", initial: null },
);

const emit = defineEmits<{
  close: [];
  submit: [{ display_name: string; pin?: string }];
}>();

const form = reactive({ display_name: "", pin: "" });

// Reset from initial on every open (never show the previous nickname).
watch(
  () => [props.show, props.initial] as const,
  () => {
    if (!props.show) return;
    form.display_name = props.initial?.display_name ?? "";
    form.pin = "";
  },
  { immediate: true },
);

function close(): void {
  emit("close");
}

function submit(): void {
  if (!canSubmit()) return;
  emit("submit", {
    display_name: form.display_name.trim(),
    pin: form.pin === "" ? undefined : form.pin,
  });
}

function canSubmit(): boolean {
  return form.display_name.trim() !== "" && !props.saving;
}
</script>
