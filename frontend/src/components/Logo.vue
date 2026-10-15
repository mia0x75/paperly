<template>
  <router-link :to="to" class="font-title text-5xl font-bold text-foreground no-underline sm:text-6xl">
    <span>&gt;&nbsp;</span>Paperly
  </router-link>
</template>

<script setup lang="ts">
/**
 * Logo -- brand mark: `> Paperly` (prefix `> ` + brand name, no suffix).
 * Font: Jersey 15 (--font-title).
 *
 * `to` is decided by the caller (parent `/`, kid `/kid`): hardcoding `/` would strand
 * a kid on the parent home with no switcher to get back.
 */
withDefaults(defineProps<{ to?: string }>(), { to: "/" });
</script>