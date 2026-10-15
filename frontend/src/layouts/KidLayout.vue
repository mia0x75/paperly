<template>
  <Layout variant="kid">
    <router-view />
  </Layout>
</template>

<script setup lang="ts">
import Layout from "./Layout.vue";

/**
 * KidLayout — student-side route shell.
 * Uses layouts/Layout.vue with an inner router-view.
 *
 * variant="kid": top bar takes the child's view — the settings menu keeps only the
 * subjects / knowledge-points items, sharing the popover behavior with the parent side.
 * A variant rather than a duplicated layout: the top bar, theme, Logo and Footer are
 * shared pieces that would immediately diverge if copied.
 */
</script>
