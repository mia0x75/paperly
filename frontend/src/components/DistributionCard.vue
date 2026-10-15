<template>
  <SubjectDonut
    data-testid="distribution-card"
    icon="pie_chart"
    :title="title"
    :items="items"
    :error="error"
    @retry="load"
  >
    <!-- Card-level filter is parent-side only (kid side uses SubjectDonut directly).
         No "全部" option: always scoped to one kid, smallest active id by default. -->
    <template #action>
      <KidScopeFilter v-model="kidId" :allow-all="false" />
    </template>
  </SubjectDonut>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { storeToRefs } from "pinia";
import KidScopeFilter from "./KidScopeFilter.vue";
import SubjectDonut from "./SubjectDonut.vue";
import { dashboardApi } from "../api/dashboard";
import { useKidStore } from "../stores/kid";
import { KID_STATUS, type SubjectShare } from "../api/types";

/**
 * DistributionCard -- parent home page "exam distribution" card (self-fetching).
 *
 * Always scoped to one kid (smallest active id by default, no "all" option);
 * switching re-fetches only this card (GET /api/dashboard/distribution?kid_id=N).
 * The kid side doesn't use this (feeds SubjectDonut directly).
 */
withDefaults(defineProps<{ title?: string }>(), { title: "试卷分布" });

const { list } = storeToRefs(useKidStore());
const activeKids = computed(() => list.value.filter((k) => k.status === KID_STATUS.ACTIVE));

const kidId = ref<number | null>(null);
const items = ref<SubjectShare[]>([]);
const error = ref("");

async function load(): Promise<void> {
  error.value = "";
  try {
    // The contract guarantees an array; fall back to [] (test stubs may return undefined).
    items.value = (await dashboardApi.distribution(kidId.value ?? undefined)) ?? [];
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
    items.value = [];
  }
}

// Default to the smallest active kid; heal a stale pick (null is only the pre-init state).
watch(
  activeKids,
  (kids) => {
    if (kids.length === 0) return;
    if (kidId.value === null || !kids.some((k) => k.id === kidId.value)) {
      kidId.value = Math.min(...kids.map((k) => k.id));
    }
  },
  { immediate: true },
);

// Re-fetch on scope change; the initial null is skipped (the watch above sets a kid first,
// triggering exactly one load, never an "all" request).
watch(
  kidId,
  () => {
    if (kidId.value !== null) void load();
  },
  { immediate: true },
);
</script>
