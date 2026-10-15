<template>
  <div ref="root" class="relative" @keydown.esc="close">
    <button
      ref="trigger"
      type="button"
      :class="triggerClass"
      :aria-expanded="open"
      :title="title"
      @click="toggle"
    >
      <slot name="trigger" :open="open" />
    </button>

    <!-- Overlay positioned inside root, not teleported: tests mount directly and
         querySelector can't reach teleported content (see test/mount.ts). -->
    <div
      v-if="open"
      :class="panelClass"
      role="menu"
      @click="onPanelClick"
    >
      <slot :close="close" />
    </div>

    <!-- Close on outside click: a full-screen layer, not a document listener — it dies
         with the component, never leaks. -->
    <div v-if="open" class="fixed inset-0 z-30" @click="close" />
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { cn } from "../../lib/utils";

/**
 * DropdownMenu: an in-repo self-contained dropdown (trigger + floating panel).
 *
 * Not a provider-based library (radix-vue needs injection; page-level tests mount
 * without App.vue, so injection fails — see AGENTS common traps). Zero dependencies.
 *
 * Close on: Esc, click outside, or any click inside the panel.
 */
const props = withDefaults(
  defineProps<{
    /** Trigger class (size/colors are the caller's concern; only interaction state is added here). */
    triggerClass?: string;
    /** Panel class. Defaults to right-aligned, below the trigger. */
    panelClass?: string;
    /**
     * Panel placement.
     *   end      — absolute, right-aligned to the trigger (default; good inside cards).
     *   viewport — below `sm`, pins to the viewport edges (inset-x-4) so a panel opened
     *              from a top-bar button near the right edge cannot run off the left;
     *              from sm up it falls back to the end alignment.
     */
    panelAlign?: "end" | "viewport";
    /** Trigger title tooltip. */
    title?: string;
    /** Whether expanded by default. */
    defaultOpen?: boolean;
  }>(),
  {
    triggerClass: "",
    panelClass: "w-64",
    panelAlign: "end",
    title: "",
    defaultOpen: false,
  },
);

const emit = defineEmits<{ "update:open": [open: boolean] }>();

const root = ref<HTMLElement | null>(null);
const trigger = ref<HTMLButtonElement | null>(null);
const open = ref(props.defaultOpen);

function toggle(): void {
  open.value = !open.value;
}

function close(): void {
  if (!open.value) return;
  open.value = false;
  trigger.value?.focus();
}

/** Any panel click collapses it: menu items are one-shot; staying would only cover content. */
function onPanelClick(): void {
  close();
}

watch(open, (v) => emit("update:open", v));

// Collapse after route change, or it floats above the new page.
onMounted(() => {
  // Tests under happy-dom do not emit hashchange; listen only in a real browser.
  if (typeof window === "undefined") return;
  window.addEventListener("popstate", close);
});

onBeforeUnmount(() => {
  if (typeof window === "undefined") return;
  window.removeEventListener("popstate", close);
});

const TRIGGER_BASE =
  "inline-flex items-center gap-2 rounded-lg border border-border/50 bg-control text-control-foreground text-sm " +
  "transition-all duration-150 ease-out outline-none " +
  "hover:bg-accent hover:text-accent-foreground active:scale-[0.98] " +
  "focus-visible:border-ring focus-visible:ring-ring focus-visible:ring-2";

const triggerClass = computed(() => cn(TRIGGER_BASE, props.triggerClass));

/**
 * MOBILE_VIEWPORT pins the panel to viewport edges below sm (a right-edge top-bar
 * trigger can't run off-screen). Appended last to win via tailwind-merge.
 */
const MOBILE_VIEWPORT = "max-sm:fixed max-sm:inset-x-4 max-sm:top-[4.5rem] max-sm:mt-0 max-sm:w-auto";

const panelClass = computed(() =>
  cn(
    "bg-popover absolute right-0 z-40 mt-2 overflow-hidden rounded-xl border border-border/50 p-1",
    "shadow-md",
    props.panelClass,
    props.panelAlign === "viewport" ? MOBILE_VIEWPORT : "",
  ),
);
</script>
