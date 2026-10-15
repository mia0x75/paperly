<!-- Reserved: not currently referenced. Kept intentionally for future use; do not delete as dead code. -->
<template>
  <input :class="inputClass" :value="modelValue" v-bind="$attrs" @input="onInput" />
</template>

<script setup lang="ts">
import { computed, useAttrs } from "vue";
import { cn } from "../../lib/utils";

/**
 * Input in shadcn/ui new-york style. v-model binds a string/number (native input).
 */
const props = defineProps<{ modelValue?: string | number }>();
const emit = defineEmits<{ "update:modelValue": [value: string] }>();
defineOptions({ inheritAttrs: false });

const attrs = useAttrs();
const inputClass = computed(() =>
  cn(
    "flex h-9 w-full min-w-0 rounded-md border bg-control text-control-foreground px-3 py-1 text-sm shadow-xs transition-[color,box-shadow] outline-none",
    "placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-0",
    "disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50",
    attrs.class as string | undefined,
  ),
);

function onInput(event: Event): void {
  emit("update:modelValue", (event.target as HTMLInputElement).value);
}
</script>
