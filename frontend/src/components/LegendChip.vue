<template>
  <!-- Bare mode: standalone status dot for table/list rows (the label rides on
       title/aria-label fallthrough attrs). Pill mode: dot + label chip for
       chart/status legends (chip bg only on mobile, transparent from sm up). -->
  <span
    v-if="bare"
    class="inline-block size-2 shrink-0 rounded-full"
    :class="dotClass"
    :style="dotStyle"
  />
  <span
    v-else
    class="hover:bg-accent hover:text-accent-foreground inline-flex items-center gap-1 rounded-xl bg-accent/60 px-2.5 py-1 sm:bg-transparent"
  >
    <span class="inline-block size-2 shrink-0 rounded-full" :class="dotClass" :style="dotStyle" />
    <slot />
  </span>
</template>

<script setup lang="ts">
import { computed, type CSSProperties } from "vue";

/**
 * LegendChip — the shared dot (+label) marker for chart/table/status legends.
 *
 * Two shapes, one contract: `dotClass` for token classes (bg-success/…) or
 * `dotColor` for data-driven colors (chart series). Pill mode standardizes on
 * gap-1/px-2.5 — StorageTrendCard's gap-1.5/px-2 differed by 2px, unified here.
 */
const props = withDefaults(
  defineProps<{
    /** Bare status dot without the pill. */
    bare?: boolean;
    /** Token class for the dot (e.g. "bg-success"). */
    dotClass?: string;
    /** Raw color for the dot (e.g. chart series color). */
    dotColor?: string;
  }>(),
  { bare: false, dotClass: "", dotColor: "" },
);

const dotStyle = computed<CSSProperties | undefined>(() =>
  props.dotColor === "" ? undefined : { backgroundColor: props.dotColor },
);
</script>
