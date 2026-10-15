<template>
  <div ref="root" data-datepicker-root class="relative" @keydown.esc="close">
    <button
      ref="triggerEl"
      type="button"
      :disabled="disabled"
      :class="triggerClass"
      :aria-expanded="open"
      aria-haspopup="dialog"
      @click="toggle"
    >
      <Icon name="calendar_today" :size="16" class="text-muted-foreground shrink-0" />
      <span :class="cn('truncate', !modelValue && 'text-muted-foreground')">
        {{ modelValue ? display : placeholder }}
      </span>
      <span
        v-if="clearable && modelValue && !disabled"
        role="button"
        aria-label="清除日期"
        class="text-muted-foreground hover:text-foreground ml-auto shrink-0"
        @click.stop="clear"
      >
        <Icon name="close" :size="14" />
      </span>
      <Icon v-else name="expand_more" :size="16" class="text-muted-foreground ml-auto shrink-0" />
    </button>

    <!-- The panel is absolutely positioned inside root rather than teleported:
         page-level tests mount the component directly, so after teleporting the
         node cannot be reached; this also avoids happy-dom's missing Portal target. -->
    <!-- Same as Select: the panel is always mounted and toggled via hidden, so
         tests need not await a microtask.
         @click.prevent same as Select: callers place the control inside a <label>,
         so a click on the overlay is treated as label activation and re-dispatched
         to the trigger, reopening it right after close; stopPropagation does not
         stop it (the re-dispatched click goes straight to the trigger), so
         preventDefault is required. -->
    <div
      role="dialog"
      :hidden="!open"
      class="bg-popover border-border/50 shadow-md absolute left-0 z-40 mt-2 rounded-xl border"
      @click.prevent.stop
    >
      <Calendar
        :model-value="calendarValue"
        :min-value="minCalendar"
        :max-value="maxCalendar"
        @update:model-value="pick"
      />
    </div>

    <!-- @click.prevent like Select: label forwarding only honors defaultPrevented. -->
    <div v-if="open" class="fixed inset-0 z-30" @click.prevent.stop="close" />
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from "vue";
import { parseDate, type CalendarDate, type DateValue } from "@internationalized/date";
import Icon from "../Icon.vue";
import Calendar from "./Calendar.vue";
import { cn } from "../../lib/utils";

/**
 * DatePicker: a string-model date picker (YYYY-MM-DD) with a themed popup calendar.
 *
 * Boundary conversion lives here alone: radix-vue needs CalendarDate inside, pages
 * keep string `v-model` outside — @internationalized/date never leaks into business
 * code. Replaces native input[type=date] (unstylable white/blue popup under dark).
 */
const props = withDefaults(
  defineProps<{
    /** YYYY-MM-DD; an empty string means nothing selected. */
    modelValue: string;
    placeholder?: string;
    disabled?: boolean;
    clearable?: boolean;
    minValue?: string;
    maxValue?: string;
    /** Additional trigger class (width/height are the caller's). */
    triggerClass?: string;
    /** Whether the display format is "YYYY年M月D日", default true. */
    zhFormat?: boolean;
  }>(),
  { placeholder: "选择日期", disabled: false, clearable: false, zhFormat: true },
);

const emit = defineEmits<{
  "update:modelValue": [value: string];
  change: [value: string];
}>();

const TRIGGER_BASE =
  "flex w-full items-center gap-2 rounded-md border bg-control text-control-foreground px-3 text-left text-sm " +
  "transition-all duration-150 ease-out outline-none " +
  "focus-visible:border-ring focus-visible:ring-ring focus-visible:ring-2 " +
  "disabled:cursor-not-allowed disabled:opacity-50";

const root = ref<HTMLElement | null>(null);
const triggerEl = ref<HTMLButtonElement | null>(null);
const open = ref(false);

const triggerClass = computed(() =>
  cn(TRIGGER_BASE, "h-9", props.disabled && "cursor-not-allowed opacity-50", props.triggerClass),
);

// Parse failure degrades to "nothing selected", never throws (dirty manual input / deep links).
function toCalendar(s: string): CalendarDate | undefined {
  if (!s) return undefined;
  try {
    return parseDate(s);
  } catch {
    return undefined;
  }
}

const calendarValue = computed(() => toCalendar(props.modelValue));
const minCalendar = computed(() => toCalendar(props.minValue ?? ""));
const maxCalendar = computed(() => toCalendar(props.maxValue ?? ""));

const display = computed(() => {
  const c = toCalendar(props.modelValue);
  if (!c) return "";
  if (!props.zhFormat) return c.toString();
  return `${c.year}年${c.month}月${c.day}日`;
});

function toggle(): void {
  if (props.disabled) return;
  open.value = !open.value;
}

function pick(value: DateValue): void {
  const s = value.toString();
  emit("update:modelValue", s);
  emit("change", s);
  close();
}

function clear(): void {
  emit("update:modelValue", "");
  emit("change", "");
}

function close(): void {
  if (!open.value) return;
  open.value = false;
  triggerEl.value?.focus();
}

onBeforeUnmount(() => {
  open.value = false;
});
</script>
