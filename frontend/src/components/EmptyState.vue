<template>
  <div class="flex flex-col items-center justify-center gap-3 py-10 text-center">
    <Icon v-if="icon" :name="icon" :size="32" class="text-muted-foreground" />
    <div>
      <p class="m-0 text-sm font-medium">{{ title }}</p>
      <p v-if="description" class="text-muted-foreground m-0 mt-1 text-sm">{{ description }}</p>
    </div>
    <Button v-if="actionLabel" variant="outline" @click="emit('action')">{{ actionLabel }}</Button>
  </div>
</template>

<script setup lang="ts">
import Button from "./ui/Button.vue";
import Icon, { type IconName } from "./Icon.vue";

/**
 * EmptyState -- unified empty state: icon + title + description + optional call-to-action.
 * The action goes through emit, not built-in navigation: routing stays with the page.
 */
withDefaults(
  defineProps<{
    icon?: IconName;
    title: string;
    description?: string;
    actionLabel?: string;
  }>(),
  {},
);
const emit = defineEmits<{ action: [] }>();
</script>
