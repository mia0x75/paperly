<template>
  <div ref="root" data-select-root class="relative" @keydown.esc="close">
    <button
      ref="trigger"
      type="button"
      role="combobox"
      :aria-expanded="open"
      :aria-controls="listboxId"
      :aria-haspopup="'listbox'"
      :disabled="disabled"
      :class="triggerClass"
      @click="toggle"
      @keydown.down.prevent="openList(0)"
      @keydown.up.prevent="openList(options.length - 1)"
      @keydown.enter.prevent="toggle"
      @keydown.space.prevent="toggle"
    >
      <span :class="cn('truncate', !selected && 'text-muted-foreground')">
        {{ selected ? selected.label : placeholder }}
      </span>
      <Icon
        name="expand_more"
        :size="16"
        class="text-muted-foreground ml-auto shrink-0 transition-transform duration-150"
        :class="open && 'rotate-180'"
      />
    </button>

    <!-- The overlay is absolutely positioned inside root rather than teleported:
         in-repo tests mount the page directly, and after teleporting to body
         querySelector cannot reach it (same handling as DropdownMenu). -->
    <!-- The panel is always mounted, toggled only via hidden.
         Why not v-if: once the panel is controlled by v-if, a test opening the
         trigger must await a Vue microtask before finding options; but in this
         self-contained component "open -> pick an option" is synchronous. Using
         hidden keeps options always queryable, so the interaction logic need not
         bend for tests. -->
    <ul
      :id="listboxId"
      role="listbox"
      :hidden="!open"
      :class="panelClass"
      @click.prevent.stop
      @keydown.down.prevent="move(1)"
      @keydown.up.prevent="move(-1)"
      @keydown.enter.prevent="commitActive"
      @keydown.home.prevent="setActive(0)"
      @keydown.end.prevent="setActive(options.length - 1)"
    >
      <li v-if="options.length === 0" class="text-muted-foreground px-2 py-3 text-center text-xs">
        无可选项
      </li>
      <li
        v-for="(opt, i) in options"
        :key="String(opt.value)"
        role="option"
        :aria-selected="isSelected(opt)"
        :aria-disabled="opt.disabled || undefined"
        :data-value="String(opt.value)"
        :class="optionClass(opt, i)"
        @click="pick(opt)"
        @mousemove="active = i"
      >
        <span class="flex-1 truncate">{{ opt.label }}</span>
        <Icon v-if="isSelected(opt)" name="check" :size="16" class="shrink-0" />
      </li>
    </ul>

    <!-- Close on click outside the panel: a full-screen transparent layer that
         disappears with the component and never leaks a global listener. -->
    <!-- @click.prevent is crucial: callers commonly place this control inside a
         <label>, and the browser treats a click landing on any descendant of the
         label as label activation, re-dispatching a click to the labeled trigger;
         thus after close it is immediately toggled open again — the symptom being
         "clicking blank space cannot close it". Note: @click.stop does not stop
         this. The label's re-dispatched click goes straight to the trigger, not
         through the overlay, so stopping propagation is ineffective against it;
         Chrome's label forwarding only checks defaultPrevented, so preventDefault
         is required. (The top bar KidSwitcher is not inside a label, so it has no
         such issue.) -->
    <div v-if="open" class="fixed inset-0 z-30" @click.prevent.stop="close" />
  </div>
</template>

<script setup lang="ts" generic="T extends string | number">
import { computed, onBeforeUnmount, ref, useId, watch } from "vue";
import Icon from "../Icon.vue";
import { cn } from "../../lib/utils";
import type { SelectOption } from "../../lib/ui";

/**
 * Select: a menu-style dropdown replacing the native <select>.
 *
 * Not native: the OS renders its popup detached from the theme (jarring white/blue
 * block on dark) with no customizable selected state. Not radix-vue Select: its
 * content portals to body, unreachable to page-level tests (see DropdownMenu).
 * Same overlay language as the top-bar dropdowns (solid bg-popover + thin border).
 */
const props = withDefaults(
  defineProps<{
    modelValue: T | null;
    options: readonly SelectOption<T>[];
    placeholder?: string;
    disabled?: boolean;
    /** Additional trigger class (height/width are the caller's). */
    triggerClass?: string;
    /** Additional overlay class. */
    panelClass?: string;
  }>(),
  { placeholder: "请选择", disabled: false },
);

const emit = defineEmits<{
  "update:modelValue": [value: T];
  change: [value: T];
}>();

const TRIGGER_BASE =
  "flex w-full items-center gap-2 rounded-md border bg-control text-control-foreground px-3 py-2 text-left text-sm " +
  "transition-all duration-150 ease-out outline-none " +
  "focus-visible:border-ring focus-visible:ring-ring focus-visible:ring-2 " +
  "focus-visible:ring-offset-0 " +
  "disabled:cursor-not-allowed disabled:opacity-50";

const PANEL_BASE =
  "bg-popover absolute left-0 z-40 mt-2 max-h-64 w-full overflow-y-auto rounded-xl " +
  "border border-border/50 p-1 shadow-md";

const root = ref<HTMLElement | null>(null);
const trigger = ref<HTMLButtonElement | null>(null);
const open = ref(false);
const active = ref(-1);

// useId, not a self-incrementing counter: <script setup> top-level re-runs per instance,
// resetting a counter to collide on the same id.
const listboxId = `select-listbox-${useId()}`;

const selected = computed(() => props.options.find((o) => o.value === props.modelValue) ?? null);

const triggerClass = computed(() =>
  cn(TRIGGER_BASE, "h-9 justify-between", props.disabled && "cursor-not-allowed opacity-50", props.triggerClass),
);
const panelClass = computed(() => cn(PANEL_BASE, props.panelClass));

function isSelected(opt: SelectOption<T>): boolean {
  return opt.value === props.modelValue;
}

function optionClass(opt: SelectOption<T>, i: number): string {
  return cn(
    "flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm transition-colors",
    i === active.value && "bg-accent text-accent-foreground",
    isSelected(opt) && "font-medium",
    opt.disabled && "pointer-events-none opacity-40",
  );
}

function toggle(): void {
  if (props.disabled) return;
  if (open.value) close();
  else openList(Math.max(0, props.options.findIndex((o) => o.value === props.modelValue)));
}

function openList(index: number): void {
  if (props.disabled) return;
  open.value = true;
  setActive(index);
}

function setActive(index: number): void {
  const n = props.options.length;
  if (n === 0) {
    active.value = -1;
    return;
  }
  active.value = ((index % n) + n) % n;
}

function move(delta: number): void {
  if (!open.value) {
    openList(0);
    return;
  }
  setActive(active.value + delta);
}

function commitActive(): void {
  const opt = props.options[active.value];
  if (opt && !opt.disabled) pick(opt);
}

function pick(opt: SelectOption<T>): void {
  if (opt.disabled) return;
  emit("update:modelValue", opt.value);
  emit("change", opt.value);
  close();
}

function close(): void {
  if (!open.value) return;
  open.value = false;
  active.value = -1;
  trigger.value?.focus();
}

// External value change (e.g. grade prefill on kid switch) moves the highlight along.
watch(
  () => props.modelValue,
  () => {
    if (open.value) setActive(props.options.findIndex((o) => o.value === props.modelValue));
  },
);

onBeforeUnmount(() => {
  open.value = false;
});
</script>
