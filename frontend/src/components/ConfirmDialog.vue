<template>
  <Modal :show="show" :title="title" @close="emit('cancel')">
    <p class="m-0">{{ message }}</p>
    <template #footer>
      <Button variant="destructive" :loading="loading" class="w-full sm:w-auto" @click="emit('confirm')">
        {{ confirmLabel }}
      </Button>
      <Button variant="outline" :disabled="loading" class="w-full sm:w-auto" @click="emit('cancel')">取消</Button>
    </template>
  </Modal>
</template>

<script setup lang="ts">
import Button from "./ui/Button.vue";
import Modal from "./Modal.vue";

/**
 * ConfirmDialog -- unified confirmation dialog (for destructive ops like deletion).
 * Based on the in-repo Modal; the confirm button is destructive.
 */
withDefaults(
  defineProps<{
    show: boolean;
    title: string;
    message: string;
    confirmLabel?: string;
    loading?: boolean;
  }>(),
  { confirmLabel: "确认删除" },
);
const emit = defineEmits<{ confirm: []; cancel: [] }>();
</script>
