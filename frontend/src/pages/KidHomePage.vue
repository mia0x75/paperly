<template>
  <!-- Content only; no top bar of its own anymore. The kid page already renders a
       nav via KidLayout(variant="kid"), and this page used to draw a second
       Logo + exams + bell + theme, so two nearly identical nav rows appeared on
       screen. Now this div/main/header wrapper is gone entirely; the top bar and
       Footer are handled by the layout. -->
  <div class="flex flex-col gap-6">
    <ErrorState v-if="error" :message="error" @retry="reload" />

    <DashboardPanels
      v-else-if="dashboard"
      :data="dashboard"
      :kid-id="dashboard.kid.id"
      :trend-days="trendDays"
      is-kid
      @update:trend-days="onTrendDays"
    />

    <p v-else class="text-muted-foreground text-sm">加载中…</p>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import DashboardPanels from "../components/DashboardPanels.vue";
import ErrorState from "../components/ErrorState.vue";
import { authApi } from "../api/auth";
import { dashboardApi } from "../api/dashboard";
import { useKidSessionStore } from "../stores/kidSession";
import type { Dashboard } from "../api/types";

/**
 * KidHomePage: the kid home page (`/kid`).
 *
 * It shares DashboardPanels with the parent side, so ROW 1~4 behave the same. Two
 * differences: kidID comes from the JWT claims (never specified here); no ROW 5
 * system monitoring (parent-only ops view). No own top bar (KidLayout owns it).
 *
 * ## Where the data comes from (JWT-driven)
 *
 *   - GET /api/kid/dashboard — the aggregate panel (requireKid, kid_id from JWT);
 *   - GET /api/me            — the identity profile (kid audience returns MeKid).
 *
 * The token attaches automatically; components never read storage. Token-less /kid/*
 * goes to /kid/login via the guard, so reaching here implies a session. Mid-flight
 * expiry (40100) clears the token and redirects to /kid/login; this page only shows
 * the error state.
 */
const kidSession = useKidSessionStore();

const trendDays = ref(30);
const dashboard = ref<Dashboard | null>(null);
const error = ref("");

function onTrendDays(days: number): void {
  trendDays.value = days;
  void reload();
}

async function reload(): Promise<void> {
  error.value = "";
  try {
    // Fetch aggregate + identity in parallel; any failure errors the page
    // (40100 redirects via the request layer).
    const [data, me] = await Promise.all([
      dashboardApi.kidOverview(trendDays.value),
      authApi.meKid(),
    ]);
    dashboard.value = data;
    // Name/kid_id/grade all come from one /api/me call, stored in session so top bar /
    // list / create stay in sync.
    kidSession.setKidName(me.name ?? "");
    kidSession.setKidId(me.id ?? null);
    kidSession.setGrade(me.grade ?? 0);
    kidSession.setTrack(me.track ?? 0);
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
    dashboard.value = null;
  }
}

onMounted(reload);
</script>
