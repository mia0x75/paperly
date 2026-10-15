<template>
  <span :class="classes"><slot /></span>
</template>

<script setup lang="ts">
import { computed } from "vue";

const props = withDefaults(
  defineProps<{
    variant?: "default" | "secondary" | "destructive" | "outline" | "success" | "warning";
  }>(),
  { variant: "default" },
);

const VARIANTS: Record<string, string> = {
  // default is currently unreferenced (all call sites pass an explicit variant);
  // kept intentionally as the fallback, same reserve policy as components/ui/.
  default: "border-transparent bg-primary text-primary-foreground",
  secondary: "border-transparent bg-secondary text-secondary-foreground",
  destructive: "border-transparent bg-destructive text-destructive-foreground",
  outline: "text-foreground border",
  success: "border-transparent bg-success text-success-foreground",
  warning: "border-transparent bg-warning text-warning-foreground",
};

const classes = computed(() => [
  "inline-flex items-center justify-center rounded-md border px-2 py-1 text-xs font-normal",
  "w-fit whitespace-nowrap shrink-0 gap-1",
  VARIANTS[props.variant],
]);
</script>
