<!-- Reserved: not currently referenced. Kept intentionally for future use; do not delete as dead code. -->
<template>
  <div :class="cardClass">
    <slot />
  </div>
</template>

<script setup lang="ts">
import { computed, useAttrs } from "vue";
import { cn } from "../../lib/utils";

/** Card in shadcn/ui new-york style: solid background, 1px border, 14px radius
 *  (no glass blur — see AGENTS.md opacity rule). */
const attrs = useAttrs();
const cardClass = computed(() =>
  cn(
    "bg-card text-card-foreground flex flex-col gap-6 rounded-xl border py-6 shadow-sm",
    attrs.class as string | undefined,
  ),
);
</script>
