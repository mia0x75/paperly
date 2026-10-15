<!-- Reserved: not currently referenced. Kept intentionally for future use; do not delete as dead code. -->
<template>
  <span :class="badgeClass"><slot /></span>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "../../lib/utils";

/** Badge variants in shadcn/ui new-york style, plus semantic success/warning. */
const badgeVariants = cva(
  "inline-flex items-center justify-center rounded-md border px-2 py-0.5 text-xs font-medium w-fit whitespace-nowrap shrink-0 gap-1",
  {
    variants: {
      variant: {
        default: "border-transparent bg-primary text-primary-foreground",
        secondary: "border-transparent bg-secondary text-secondary-foreground",
        destructive: "border-transparent bg-destructive text-destructive-foreground",
        outline: "text-foreground border",
        success: "border-transparent bg-success text-success-foreground",
        warning: "border-transparent bg-warning text-warning-foreground",
      },
    },
    defaultVariants: { variant: "default" },
  },
);

type BadgeVariants = VariantProps<typeof badgeVariants>;
const props = withDefaults(defineProps<{ variant?: BadgeVariants["variant"] }>(), {});
const badgeClass = computed(() => cn(badgeVariants({ variant: props.variant })));
</script>
