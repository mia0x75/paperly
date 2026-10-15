<!-- Reserved: not currently referenced. Kept intentionally for future use; do not delete as dead code. -->
<template>
  <textarea :class="textareaClass" :value="modelValue" v-bind="$attrs" @input="onInput" />
</template>

<script setup lang="ts">
import { computed, useAttrs } from "vue";
import { cn } from "../../lib/utils";

/** Textarea in shadcn/ui new-york style. */
const props = defineProps<{ modelValue?: string }>();
const emit = defineEmits<{ "update:modelValue": [value: string] }>();
defineOptions({ inheritAttrs: false });

const attrs = useAttrs();
const textareaClass = computed(() =>
  cn(
    "flex min-h-16 w-full rounded-md border bg-control text-control-foreground px-3 py-2 text-sm shadow-xs outline-none",
    "placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring",
    "disabled:cursor-not-allowed disabled:opacity-50",
    attrs.class as string | undefined,
  ),
);

function onInput(event: Event): void {
  emit("update:modelValue", (event.target as HTMLTextAreaElement).value);
}
</script>
