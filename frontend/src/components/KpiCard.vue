<template>
  <div class="bg-card text-card-foreground flex flex-col gap-4 rounded-xl border py-4 shadow-sm">
    <div class="flex items-center justify-between gap-2 px-4">
      <div class="flex min-w-0 items-center gap-2 text-base font-medium">
        <Icon v-if="icon" :name="icon" :size="20" />
        <span class="truncate">{{ title }}</span>
      </div>
      <!-- Title-row right action slot; no gap when not passed -->
      <div v-if="$slots.action" class="shrink-0">
        <slot name="action" />
      </div>
    </div>

    <!-- Error first: failure and genuine 0 must not both render as 0 -->
    <div v-if="error" class="px-4">
      <ErrorState :message="error" retry-label="重试" @retry="emit('retry')" />
    </div>
    <div v-else class="flex flex-col gap-1 px-4">
      <div class="flex flex-wrap items-end gap-2">
        <span
          v-if="loading"
          class="h-9 w-24 animate-pulse rounded-md bg-muted"
          aria-hidden="true"
        />
        <span v-else class="text-3xl leading-tight font-bold tracking-tight tabular-nums">
          {{ valueText }}
        </span>
      </div>
      <div v-if="subtext" class="text-muted-foreground text-sm">{{ subtext }}</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import ErrorState from "./ErrorState.vue";
import Icon, { type IconName } from "./Icon.vue";

/**
 * KpiCard -- display shell for the first-row KPI cards.
 *
 * Pure presentational: numbers come from the caller (parent wraps it in ScopeKpiCard
 * after fetching; kid side feeds dashboard data directly).
 */
const props = withDefaults(
  defineProps<{
    title: string;
    icon?: IconName;
    /** Main number; defaults to 0 — rendering undefined would crash toLocaleString. */
    value?: number;
    subtext?: string;
    /** Loading skeleton matches the final number's shape (no height jump). */
    loading?: boolean;
    /** Non-empty error shows the error state with retry (one card's failure never spills). */
    error?: string;
  }>(),
  { value: 0, loading: false, error: "" },
);

const emit = defineEmits<{ retry: [] }>();

const valueText = computed(() => (props.value ?? 0).toLocaleString());
</script>
