<template>
  <div
    v-if="show"
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
    @click.self="emit('close')"
  >
    <div
      role="dialog"
      aria-modal="true"
      class="bg-card flex max-h-[90vh] w-full max-w-md flex-col overflow-hidden rounded-xl border p-6 shadow-md"
    >
      <h2 class="mt-0 mb-3 flex-none text-base font-semibold">{{ title }}</h2>
      <!-- Only the body scrolls: the title and footer stay pinned, so the action buttons are
           reachable even when a tall form (e.g. the kid form's subject grid) overflows 90vh.
           With flexBody the body is a flex column, so the slot content can fill it (flex-1
           min-h-0) and scroll an inner region itself; at rest there is no overflow, so the
           body itself doesn't scroll (padding is offset by equal negative margins to keep
           alignment while giving the focus ring room). -->
      <div
        data-testid="modal-body"
        class="-mx-1 -my-1 min-h-0 flex-1 overflow-y-auto px-1 py-1 text-sm"
        :class="flexBody && 'flex flex-col'"
      >
        <slot />
      </div>
      <div
        data-testid="modal-footer"
        class="mt-5 flex flex-none flex-col gap-3 border-t pt-4 sm:flex-row sm:justify-end"
      >
        <slot name="footer" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, watch } from "vue";

/**
 * Modal — an in-repo lightweight dialog.
 *
 * Deliberately not provider-based (e.g. useDialog): page-level tests mount pages
 * directly without App.vue's provider, so injection fails. In-repo renders with
 * the page — testable, no provider dependency.
 *
 * Esc close lives here once, so every overlay gets it; the listener hangs on window
 * (works without content focus) and follows the show toggle.
 *
 * Scroll lock sets overflow:hidden on <html> while open (browser preserves scroll
 * position, no offset juggling); nested modals are ref-counted.
 *
 * Layout: the panel is a flex column capped at 90vh, with the title and footer fixed
 * and only the body scrolling — so a tall form never pushes the action buttons off
 * screen.
 *
 * flexBody: when set, the body is a flex column so the slot content can fill it
 * (flex-1 min-h-0) and scroll an inner region itself. At rest there is no overflow,
 * so the body doesn't scroll. Used by the kid form so its fields stay pinned while
 * only the subject list scrolls.
 */
const props = withDefaults(defineProps<{ show: boolean; title: string; flexBody?: boolean }>(), {
  flexBody: false,
});
const emit = defineEmits<{ close: [] }>();

// Open Modal count, module-level (coordinates instances; nothing renders from it).
let openCount = 0;

function onKeydown(event: KeyboardEvent): void {
  if (event.key !== "Escape") return;
  event.preventDefault();
  emit("close");
}

function bind(): void {
  window.addEventListener("keydown", onKeydown);
}

function unbind(): void {
  window.removeEventListener("keydown", onKeydown);
}

function lockScroll(): void {
  if (openCount === 0) document.documentElement.style.overflow = "hidden";
  openCount++;
}

function unlockScroll(): void {
  if (openCount === 0) return;
  openCount--;
  if (openCount === 0) document.documentElement.style.overflow = "";
}

watch(
  () => props.show,
  (visible) => {
    if (visible) {
      bind();
      lockScroll();
    } else {
      unbind();
      unlockScroll();
    }
  },
  // immediate: a first render with show=true must also bind/lock.
  { immediate: true },
);

onBeforeUnmount(() => {
  // Unmount with show=true must not leak the listener or the lock.
  unbind();
  if (props.show) unlockScroll();
});
</script>
