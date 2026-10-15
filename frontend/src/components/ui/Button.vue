<template>
  <button :class="buttonClass" :disabled="disabled || loading" :type="type" @click="onClick">
    <!-- Loading spinner reuses the Icon component: the subset icon font is addressed
         by PUA codepoint only (no ligature table), so a literal "refresh" text string
         here would flash as the word "Refresh" in a fallback font before/without the
         ligature resolving. Same glyph, same sizing, no raw text. -->
    <span v-if="loading" class="inline-flex shrink-0 animate-spin" :style="spinStyle">
      <Icon name="refresh" :size="iconSize" />
    </span>
    <Icon v-else-if="icon" :name="icon" :size="iconSize" />
    <slot />
  </button>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "../../lib/utils";
import Icon, { type IconName } from "../Icon.vue";

/** Button variants and sizes in shadcn/ui new-york style. */
const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-all shrink-0 outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:border-ring disabled:pointer-events-none disabled:opacity-50",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground shadow-xs hover:bg-primary/90",
        destructive: "bg-destructive text-destructive-foreground shadow-xs hover:bg-destructive/90",
        outline: "border bg-control text-control-foreground shadow-xs hover:bg-accent hover:text-accent-foreground",
        secondary: "bg-secondary text-secondary-foreground shadow-xs hover:bg-secondary/80",
        ghost: "hover:bg-accent hover:text-accent-foreground",
        link: "text-primary underline-offset-4 hover:underline",
      },
      size: {
        default: "h-9 px-4 py-2",
        sm: "h-8 gap-1.5 px-3",
        lg: "h-10 px-6",
        icon: "size-9",
      },
    },
    defaultVariants: { variant: "default", size: "default" },
  },
);

type ButtonVariants = VariantProps<typeof buttonVariants>;

const props = withDefaults(
  defineProps<{
    variant?: ButtonVariants["variant"];
    size?: ButtonVariants["size"];
    type?: "button" | "submit" | "reset";
    disabled?: boolean;
    loading?: boolean;
    icon?: IconName;
    /** Extra classes merged after variant/size (tailwind-merge resolves conflicts). */
    class?: string;
  }>(),
  { type: "button" },
);

const emit = defineEmits<{ click: [event: MouseEvent] }>();

const buttonClass = computed(() =>
  cn(buttonVariants({ variant: props.variant, size: props.size }), props.class),
);
const iconSize = computed(() => (props.size === "sm" ? 14 : 16));
const spinStyle = computed(() => ({
  width: `${iconSize.value}px`,
  height: `${iconSize.value}px`,
}));

function onClick(event: MouseEvent): void {
  if (props.disabled || props.loading) return;
  emit("click", event);
}
</script>
