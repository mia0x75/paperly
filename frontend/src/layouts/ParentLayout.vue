<template>
  <Layout variant="parent">
    <router-view />
  </Layout>
</template>

<script setup lang="ts">
import Layout from "./Layout.vue";

/**
 * ParentLayout — parent-side route shell.
 * Uses layouts/Layout.vue with an inner router-view.
 *
 * variant="parent" is Layout's default; written explicitly to pair with KidLayout,
 * since deciding the perspective is this shell's job, not Layout guessing from the path.
 */
</script>
