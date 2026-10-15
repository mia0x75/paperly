<template>
  <div
    v-if="show"
    data-testid="exam-zoom"
    class="bg-background fixed inset-0 z-50 flex flex-col"
    role="dialog"
    aria-modal="true"
    aria-label="放大查看试卷页"
  >
    <div class="flex flex-wrap items-center justify-between gap-3 border-b border-border/50 px-6 py-3">
      <!-- Mobile: title would push controls to a second line — counter left, controls right on one row. -->
      <span class="hidden truncate text-sm font-medium sm:block">{{ title }}</span>
      <span class="text-sm tabular-nums sm:hidden">第 {{ pageIndex + 1 }} / {{ pageCount }} 页</span>
      <div class="flex items-center gap-2">
        <span class="text-muted-foreground hidden text-sm tabular-nums sm:block">{{ pageIndex + 1 }} / {{ pageCount }}</span>
        <div class="flex items-center gap-1">
          <Button
            variant="outline"
            size="icon"
            class="hidden size-11 sm:inline-flex sm:size-9"
            title="缩小"
            aria-label="缩小"
            :disabled="scaleIndex <= 0"
            @click="zoomOut"
          >
            <Icon name="horizontal_rule" :size="18" />
          </Button>
          <span class="text-muted-foreground hidden w-14 text-center text-sm tabular-nums sm:block">
            {{ Math.round(scale * 100) }}%
          </span>
          <Button
            variant="outline"
            size="icon"
            class="hidden size-11 sm:inline-flex sm:size-9"
            title="放大"
            aria-label="放大"
            :disabled="scaleIndex >= ZOOM_STEPS.length - 1"
            @click="zoomIn"
          >
            <Icon name="add" :size="18" />
          </Button>
          <!-- Fit height shows the whole portrait page; fit width spreads one row — each
               overflows the other way, so both are offered. Active step uses secondary:
               ghost's hover:bg-accent would read as hover, not selection. -->
          <Button
            :variant="fitMode === 'height' ? 'secondary' : 'outline'"
            size="icon"
            class="size-11 sm:size-9"
            title="适应高度"
            aria-label="适应高度"
            data-testid="fit-height"
            @click="fitTo('height')"
          >
            <Icon name="expand" :size="18" />
          </Button>
          <Button
            :variant="fitMode === 'width' ? 'secondary' : 'outline'"
            size="icon"
            class="size-11 sm:size-9"
            title="适应宽度"
            aria-label="适应宽度"
            data-testid="fit-width"
            @click="fitTo('width')"
          >
            <Icon name="expand" :size="18" class="rotate-90" />
          </Button>
        </div>
        <Button
          variant="destructive"
          size="icon"
          class="size-11 sm:size-9"
          title="关闭"
          aria-label="关闭"
          @click="emit('close')"
        >
          <Icon name="close" :size="18" />
        </Button>
      </div>
    </div>

    <div class="relative flex min-h-0 flex-1">
      <!-- Never center with items-center justify-center: on overflow, flex centering pushes
           content past the start edge and scrollTop 0 can't reach it (measured 389px cropped).
           items-start + image m-auto centers only with spare room. touch-none stops the
           browser stealing two-finger gestures as page zoom (pinch is JS-handled on the image).
           Mobile p-0 spans full width; sm+ keeps p-4. -->
      <div
        ref="zoomBox"
        data-testid="zoom-box"
        class="flex min-h-0 flex-1 touch-none items-start overflow-auto p-0 sm:p-4"
        :class="dragging ? 'cursor-grabbing' : 'cursor-grab'"
        @wheel.prevent="onWheel"
        @pointerdown="onPointerDown"
        @pointermove="onPointerMove"
        @pointerup="onPointerUp"
        @pointercancel="onPointerUp"
      >
        <img
          v-if="imageSrc !== ''"
          :src="imageSrc"
          :alt="alt"
          data-testid="zoom-image"
          class="m-auto max-w-none shrink-0 select-none"
          draggable="false"
          :style="imageStyle"
          @load="onImageLoad"
        />
        <span v-else class="text-muted-foreground m-auto text-sm">无页面</span>
      </div>

      <!-- Page flip buttons float over the image instead of taking a side column: on a
           phone every horizontal pixel counts for reading the page. Opaque bg (no
           translucency) so the exam text behind never bleeds through the control. -->
      <Button
        variant="outline"
        size="icon"
        class="absolute top-1/2 left-2 z-10 size-12 -translate-y-1/2 rounded-full shadow-lg sm:size-10"
        :disabled="pageIndex <= 0"
        aria-label="上一页"
        @click="emit('prev')"
      >
        <Icon name="chevron_left" :size="24" />
      </Button>

      <Button
        variant="outline"
        size="icon"
        class="absolute top-1/2 right-2 z-10 size-12 -translate-y-1/2 rounded-full shadow-lg sm:size-10"
        :disabled="pageIndex >= pageCount - 1"
        aria-label="下一页"
        @click="emit('next')"
      >
        <Icon name="chevron_right" :size="24" />
      </Button>
    </div>

    <p class="text-muted-foreground hidden pb-3 text-center text-xs sm:block">
      缩放：滚轮 / 双指 · 移动：拖动 · ← → 翻页 · Esc 关闭
    </p>
  </div>
</template>

<script setup lang="ts">
/**
 * ExamZoomOverlay -- full-screen zoom overlay for exam page images.
 *
 * Self-implemented, not the native Fullscreen API: the native one needs gestures, hides
 * silent Escape exits from Vue, and is untestable headless; fixed + z-50 suffices.
 *
 * Three trade-offs live in comments below; read them before changing:
 *   1. **auto margin centering**, not flex centering — overflow stays reachable.
 *   2. **explicit width**, not transform:scale() — transform leaves the layout box
 *      (and thus the scroll range) at the original size.
 *   3. **wheel zoom anchored to the cursor** — naive stepping makes the view jump.
 */
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import Button from "./ui/Button.vue";
import Icon from "./Icon.vue";

const props = defineProps<{
  show: boolean;
  title: string;
  imageSrc: string;
  alt: string;
  pageIndex: number;
  pageCount: number;
}>();

const emit = defineEmits<{ close: []; prev: []; next: [] }>();

/**
 * ZOOM_STEPS are the zoom-magnification steps. Index 0 = 100% (fit to the current axis),
 * later indices zoom in further.
 *
 * Discrete steps, not a slider: fixed steps land on the same position every time and
 * make keyboard stepping easy. Upper bound 400% — beyond that only blurs pixels.
 */
const ZOOM_STEPS = [1, 1.5, 2, 3, 4] as const;

/** Axis filled at 100% (whole portrait page visible = fit height). */
type FitMode = "width" | "height";

const scaleIndex = ref(0);
const fitMode = ref<FitMode>("height");
/** Image natural pixels (reused across fit-axis switches without waiting for load). */
const naturalW = ref(0);
const naturalH = ref(0);
/** Display size at the 100% step (multiplier applies on top). */
const fitW = ref(0);
const fitH = ref(0);
const zoomBox = ref<HTMLElement | null>(null);
const dragging = ref(false);

const scale = computed<number>(() => ZOOM_STEPS[scaleIndex.value] ?? 1);

/**
 * Width only, never height: rounding errors in fitW/fitH would distort the image;
 * `height: auto` is robust. The img must also carry `max-w-none` — preflight's
 * `img { max-width: 100% }` would clamp the enlarged width ("zoomed but squashed").
 * At naturalW/H 0 (not loaded yet), fall back to object-contain (no 0x0 first frame).
 */
const imageStyle = computed(() => {
  if (fitW.value <= 0 || fitH.value <= 0) {
    return { maxWidth: "100%", maxHeight: "100%", objectFit: "contain" as const };
  }
  return { width: `${Math.round(fitW.value * scale.value)}px` };
});

/** Display size at the 100% step, from natural pixels + fit axis + container. */
function measureFit(): void {
  const box = zoomBox.value;
  if (box === null || naturalW.value <= 0 || naturalH.value <= 0) return;
  const cw = box.clientWidth;
  const ch = box.clientHeight;
  if (cw <= 0 || ch <= 0) return;
  const ratio = fitMode.value === "height" ? ch / naturalH.value : cw / naturalW.value;
  fitW.value = naturalW.value * ratio;
  fitH.value = naturalH.value * ratio;
}

function onImageLoad(e: Event): void {
  const img = e.target as HTMLImageElement;
  naturalW.value = img.naturalWidth;
  naturalH.value = img.naturalHeight;
  measureFit();
}

function zoomIn(): void {
  if (scaleIndex.value < ZOOM_STEPS.length - 1) scaleIndex.value += 1;
}

function zoomOut(): void {
  if (scaleIndex.value > 0) scaleIndex.value -= 1;
}

/** Switch fit axis: reset to 100%, scroll back to top-left (old position is meaningless). */
function fitTo(mode: FitMode): void {
  fitMode.value = mode;
  scaleIndex.value = 0;
  measureFit();
  scrollToOrigin();
}

function scrollToOrigin(): void {
  zoomBox.value?.scrollTo({ top: 0, left: 0 });
}

/**
 * Step the zoom to an explicit index, anchored at a viewport point.
 *
 * Unanchored stepping scales around the container's top-left and the viewed row runs
 * away; compensate with scroll += relative position x (new width - old width), measured
 * off the image rect (holds centered or overflowed). Shared by wheel and pinch zoom.
 */
function stepTo(index: number, anchorX: number, anchorY: number): void {
  const box = zoomBox.value;
  const img = box?.querySelector("img");
  // Not `instanceof HTMLImageElement`: fails silently across realms (no global in
  // tests; cross-iframe is another realm too) — duck-type instead.
  if (box === null || box === undefined || img === null || img === undefined) return;
  const imgRect = img.getBoundingClientRect();
  if (imgRect.width <= 0 || imgRect.height <= 0) return;

  const prevScale = scale.value;
  scaleIndex.value = Math.max(0, Math.min(ZOOM_STEPS.length - 1, index));
  const nextScale = scale.value;
  if (nextScale === prevScale) return;

  const ratio = nextScale / prevScale;
  const rx = (anchorX - imgRect.left) / imgRect.width;
  const ry = (anchorY - imgRect.top) / imgRect.height;
  box.scrollLeft += imgRect.width * (ratio - 1) * rx;
  box.scrollTop += imgRect.height * (ratio - 1) * ry;
}

/** Wheel zoom, anchored to the point under the cursor. */
function onWheel(e: WheelEvent): void {
  stepTo(scaleIndex.value + (e.deltaY < 0 ? 1 : -1), e.clientX, e.clientY);
}

/** Active contact points by pointerId (one-finger pan + two-finger pinch). */
const pointers = new Map<number, { x: number; y: number }>();
/** Set once a gesture involves two fingers: suppresses the edge-swipe page flip on release. */
let gesturePinched = false;
/** Pinch baseline: finger distance + image scale at second-finger touchdown. */
let pinchStartDist = 0;
let pinchStartScale = 1;

let dragFrom = { x: 0, y: 0, left: 0, top: 0 };

function pinchDist(): number {
  const pts = [...pointers.values()];
  const a = pts[0];
  const b = pts[1];
  if (a === undefined || b === undefined) return 0;
  return Math.hypot(a.x - b.x, a.y - b.y);
}

/**
 * Two-finger pinch-to-zoom on the image only (`touch-none` hides the gesture from the
 * browser, so buttons never scale). Finger-spread maps onto ZOOM_STEPS at the live
 * midpoint — pinch, wheel and buttons always agree on sizes.
 */
function onPointerDown(e: PointerEvent): void {
  const box = zoomBox.value;
  if (box === null || e.button !== 0) return;
  pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
  (e.currentTarget as HTMLElement | null)?.setPointerCapture?.(e.pointerId);
  if (pointers.size === 2) {
    // Second finger down: stop panning, start pinching from the current scale.
    gesturePinched = true;
    dragging.value = false;
    pinchStartDist = pinchDist();
    pinchStartScale = scale.value;
    return;
  }
  if (pointers.size === 1) {
    // Hold-and-drag to pan: pointer displacement maps directly to scroll amount, threshold 0.
    gesturePinched = false;
    dragging.value = true;
    dragFrom = { x: e.clientX, y: e.clientY, left: box.scrollLeft, top: box.scrollTop };
  }
}

function onPointerMove(e: PointerEvent): void {
  const box = zoomBox.value;
  if (box === null || !pointers.has(e.pointerId)) return;
  pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
  if (pointers.size === 2) {
    const dist = pinchDist();
    if (pinchStartDist > 0 && dist > 0) {
      const target = pinchStartScale * (dist / pinchStartDist);
      let best = 0;
      let bestDiff = Number.POSITIVE_INFINITY;
      for (let i = 0; i < ZOOM_STEPS.length; i++) {
        const step = ZOOM_STEPS[i] ?? 1;
        const diff = Math.abs(step - target);
        if (diff < bestDiff) {
          bestDiff = diff;
          best = i;
        }
      }
      if (best !== scaleIndex.value) {
        const pts = [...pointers.values()];
        const a = pts[0];
        const b = pts[1];
        if (a !== undefined && b !== undefined) {
          stepTo(best, (a.x + b.x) / 2, (a.y + b.y) / 2);
        }
      }
    }
    return;
  }
  if (!dragging.value) return;
  box.scrollLeft = dragFrom.left - (e.clientX - dragFrom.x);
  box.scrollTop = dragFrom.top - (e.clientY - dragFrom.y);
}

/**
 * Release: end the drag; swiping past a scroll edge flips the page (same gesture on
 * touch and mouse). A gesture that involved pinching never flips.
 */
function onPointerUp(e: PointerEvent): void {
  pointers.delete(e.pointerId);
  const box = zoomBox.value;
  if (box === null) {
    dragging.value = false;
    return;
  }
  if (pointers.size === 1) {
    // Pinch ended with one finger down: re-anchor the pan (no jump, no mid-gesture flip).
    const rest = [...pointers.values()][0];
    if (rest !== undefined) {
      dragging.value = true;
      dragFrom = { x: rest.x, y: rest.y, left: box.scrollLeft, top: box.scrollTop };
    }
    return;
  }
  dragging.value = false;
  const wasPinch = gesturePinched;
  if (pointers.size === 0) gesturePinched = false;
  if (wasPinch) return;
  const maxLeft = box.scrollWidth - box.clientWidth;
  const dx = e.clientX - dragFrom.x;
  const SWIPE = 48; // px; panning within the image below this stays a pan
  if (maxLeft <= 0) return;
  if (box.scrollLeft <= 0 && dx > SWIPE && props.pageIndex > 0) {
    emit("prev");
    return;
  }
  if (box.scrollLeft >= maxLeft - 1 && dx < -SWIPE && props.pageIndex < props.pageCount - 1) {
    emit("next");
  }
}

/**
 * Keyboard: Esc closes, arrows page, +/- zoom, 0 resets. Listener on window (not the
 * overlay div) so events arrive regardless of focus; capture keeps controls from
 * consuming +/- first.
 */
function onKey(e: KeyboardEvent): void {
  if (!props.show) return;
  switch (e.key) {
    case "Escape":
      e.preventDefault();
      emit("close");
      return;
    case "ArrowLeft":
      if (props.pageIndex > 0) {
        e.preventDefault();
        emit("prev");
      }
      return;
    case "ArrowRight":
      if (props.pageIndex < props.pageCount - 1) {
        e.preventDefault();
        emit("next");
      }
      return;
    case "+":
    case "=":
      e.preventDefault();
      zoomIn();
      return;
    case "-":
      e.preventDefault();
      zoomOut();
      return;
    case "0":
      e.preventDefault();
      fitTo("height");
      return;
    default:
  }
}

/** Re-measure the 100% baseline on resize (else it keeps the old container's size). */
function onWindowResize(): void {
  if (!props.show) return;
  measureFit();
}

// Reset on open (don't inherit the previous 300%).
watch(
  () => props.show,
  (on) => {
    if (typeof document === "undefined") return;
    // Lock body scroll (background page would scroll along unnoticed).
    document.body.style.overflow = on ? "hidden" : "";
    if (!on) return;
    scaleIndex.value = 0;
    pointers.clear();
    gesturePinched = false;
    dragging.value = false;
    // Narrow screens default to fit-width (full-width portrait); desktop keeps fit-height.
    fitMode.value = typeof window !== "undefined" && window.matchMedia("(max-width: 640px)").matches
      ? "width"
      : "height";
    naturalW.value = 0;
    naturalH.value = 0;
    fitW.value = 0;
    fitH.value = 0;
  },
);

onMounted(() => {
  if (typeof window !== "undefined") {
    window.addEventListener("keydown", onKey, true);
    window.addEventListener("resize", onWindowResize);
  }
});

onBeforeUnmount(() => {
  if (typeof window !== "undefined") {
    window.removeEventListener("keydown", onKey, true);
    window.removeEventListener("resize", onWindowResize);
  }
  if (typeof document !== "undefined") document.body.style.overflow = "";
});
</script>
