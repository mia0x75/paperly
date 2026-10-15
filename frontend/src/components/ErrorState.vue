<template>
  <div
    class="border-destructive/50 bg-destructive/10 text-destructive flex flex-col items-center gap-3 rounded-md border px-3 py-4 text-center text-sm"
  >
    <p class="m-0">{{ message }}</p>
    <Button v-if="retryLabel" variant="outline" size="sm" @click="emit('retry')">
      {{ retryLabel }}
    </Button>
  </div>
</template>

<script setup lang="ts">
import Button from "./ui/Button.vue";

/**
 * ErrorState -- unified error state: message + optional retry button.
 * The message comes from the page; this component never parses error codes.
 */
withDefaults(
  defineProps<{
    message: string;
    retryLabel?: string;
  }>(),
  {},
);
const emit = defineEmits<{ retry: [] }>();
</script>
