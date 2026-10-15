<template>
  <div class="pointer-events-none fixed inset-0 -z-10">
    <div
      class="absolute inset-0 bg-size-[20px_20px] bg-[radial-gradient(var(--pattern-dot)_1px,transparent_1px)]"
    />
    <div
      class="bg-background pointer-events-none absolute inset-0 flex items-center justify-center mask-[radial-gradient(ellipse_at_center,transparent_20%,black)]"
    />
  </div>
</template>

<script setup lang="ts">
/**
 * BackgroundPattern -- radial dot-grid background + center radial mask.
 *
 * Dots go through the --pattern-dot token, not Tailwind's `dark:` variant: the theme is
 * driven by <html class="dark"> (theme/mode.ts), while `dark:` follows the OS media
 * query — mixing them draws light dots in dark mode.
 */
</script>
