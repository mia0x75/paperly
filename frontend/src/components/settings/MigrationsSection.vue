<template>
  <section id="section-migrations" class="bg-card text-card-foreground flex flex-col gap-4 rounded-xl border p-6 shadow-sm">
    <div class="flex items-center justify-between">
      <h2 class="text-base font-semibold tracking-tight">系统版本迁移与任务游标</h2>
      <Button variant="outline" icon="refresh" :loading="loading" @click="load">刷新</Button>
    </div>

    <ErrorState v-if="error !== ''" :message="error" retry-label="重试" @retry="load" />
    <div v-else-if="loading && migrations.length === 0" class="space-y-2">
      <div v-for="i in 3" :key="i" class="bg-muted animate-pulse h-10 rounded-lg" />
    </div>

    <template v-else>
      <div>
        <h3 class="text-muted-foreground mb-2 text-sm font-normal">数据库结构版本（migrations）</h3>
        <ul v-if="migrations.length > 0" class="divide-border/60 divide-y">
          <!-- Mobile: version time takes its own full-width row below; sm+: inline right. -->
          <li v-for="m in migrations" :key="m.version" class="flex flex-wrap items-center gap-x-3 gap-y-1 py-2 text-sm">
            <span class="font-mono text-sm">#{{ m.version }}</span>
            <span class="font-medium">{{ m.name }}</span>
            <span class="inline-block size-2 rounded-full bg-success" title="已应用" aria-label="已应用" role="img"></span>
            <span class="text-muted-foreground w-full text-sm font-normal tabular-nums sm:ml-auto sm:w-auto">
              {{ m.display_tag }} · {{ m.applied_at }}
            </span>
          </li>
        </ul>
        <p v-else class="text-muted-foreground m-0 text-sm">暂无迁移记录。</p>
      </div>

      <div>
        <h3 class="text-muted-foreground mb-2 text-sm font-normal">任务游标持久化（cursors）</h3>
        <ul v-if="cursors.length > 0" class="divide-border/60 divide-y">
          <li v-for="c in cursors" :key="c.task_name" class="flex flex-wrap items-center gap-3 py-2 text-sm">
            <span class="font-mono text-sm">{{ c.task_name }}</span>
            <span class="text-muted-foreground text-sm font-normal tabular-nums">last_id = {{ c.last_id }}</span>
            <span
              class="inline-block size-2 rounded-full"
              :class="c.last_status === 0 || c.last_status === 1 ? 'bg-success' : 'bg-muted-foreground/40'"
              :title="statusLabel(c.last_status)"
              :aria-label="statusLabel(c.last_status)"
              role="img"
            ></span>
            <span
              class="text-muted-foreground w-full text-sm font-normal tabular-nums sm:ml-auto sm:w-auto"
            >
              {{ c.last_run_at || "从未运行" }}
            </span>
          </li>
        </ul>
        <p v-else class="text-muted-foreground m-0 text-sm">暂无游标记录。</p>
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import Button from "../ui/Button.vue";
import ErrorState from "../ErrorState.vue";
import StatusBadge from "../StatusBadge.vue";
import { settingsApi } from "../../api/settings";
import { TASK_STATUS_TEXT } from "../../api/types";
import type { SchemaMigration, TaskCursor } from "../../api/types";

/**
 * MigrationsSection — the "migrations & cursors" section of the settings page, read-only.
 *
 * Deliberately no manual "run migration" button: migrations run at startup after a full
 * backup; a UI trigger would hand an irreversible op to a slip of the hand.
 */
const migrations = ref<SchemaMigration[]>([]);
const cursors = ref<TaskCursor[]>([]);
const loading = ref(false);
const error = ref("");

function statusLabel(s: number): string {
  return TASK_STATUS_TEXT[s] ?? `状态 ${s}`;
}

async function load(): Promise<void> {
  loading.value = true;
  error.value = "";
  try {
    const [m, c] = await Promise.all([settingsApi.migrations(), settingsApi.cursors()]);
    migrations.value = m;
    cursors.value = c;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    loading.value = false;
  }
}

onMounted(load);
</script>
