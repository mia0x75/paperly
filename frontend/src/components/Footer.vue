<template>
  <footer class="text-muted-foreground mt-8 grid grid-cols-[1fr_auto] items-end gap-2 border-t pt-4 text-xs sm:flex sm:flex-row sm:items-end sm:justify-between">
    <!-- Left: brand lines. -->
    <div class="flex flex-col">
      <span class="text-foreground text-sm">Powered by Paperly.</span>
      <span class="text-muted-foreground text-xs">Scan, Score, Succeed.</span>
    </div>

    <!-- Right: render time. The mobile grid keeps it on the tagline row, hugging the right edge. -->
    <span class="text-xs whitespace-nowrap tabular-nums">本次渲染 {{ renderMs }} ms</span>
  </footer>
</template>

<script setup lang="ts">
import { nextTick, onMounted, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { elapsedSinceRouteStart } from "../lib/renderTimer";

/**
 * Footer -- page footer.
 *
 * Render time spans routes (see lib/renderTimer.ts); no backend call — process
 * usage lives on the health page instead.
 */
const route = useRoute();
const renderMs = ref(0);

/** Wait one frame before reading the time, ensuring the new page DOM has finished patching. */
async function refreshRenderTime(): Promise<void> {
  await nextTick();
  renderMs.value = elapsedSinceRouteStart();
}

// Re-measure render time on route change (Footer is persistent and does not remount with routes).
watch(() => route.fullPath, () => {
  void refreshRenderTime();
});

onMounted(() => {
  void refreshRenderTime();
});
</script>
