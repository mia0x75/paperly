<template>
  <section id="section-storage" class="bg-card text-card-foreground flex flex-col gap-4 rounded-xl border p-6 shadow-sm">
    <div class="flex items-center justify-between">
      <h2 class="text-base font-semibold tracking-tight">存储健康与冷备份</h2>
      <Button variant="outline" icon="refresh" :loading="loading" @click="loadAll">刷新</Button>
    </div>

    <ErrorState v-if="error !== ''" :message="error" retry-label="重试" @retry="loadAll" />
    <div v-else-if="loading && !summary" class="grid grid-cols-1 gap-4 sm:grid-cols-2">
      <div v-for="i in 4" :key="i" class="bg-muted animate-pulse h-16 rounded-lg" />
    </div>

    <template v-else-if="summary">
      <dl class="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div class="rounded-lg border p-4">
          <dt class="text-muted-foreground text-sm font-normal">磁盘使用率</dt>
          <dd class="mt-1 text-2xl font-bold tracking-tight tabular-nums">
            {{ diskText }}
          </dd>
          <div v-if="summary.storage.disk_supported" class="bg-muted mt-2 h-1.5 w-full overflow-hidden rounded-full">
            <div class="bg-progress h-full" :style="{ width: diskPercent + '%' }" />
          </div>
        </div>
        <div class="rounded-lg border p-4">
          <dt class="text-muted-foreground text-sm font-normal">数据库</dt>
          <dd class="mt-1 text-2xl font-bold tracking-tight font-mono">{{ summary.database.size_human }}</dd>
          <p class="text-muted-foreground mt-1 text-sm font-normal">
            结构版本 {{ summary.database.schema_tag || "—" }}
          </p>
        </div>
        <div class="rounded-lg border p-4">
          <dt class="text-muted-foreground text-sm font-normal">图片与资产存储</dt>
          <dd class="mt-1 text-2xl font-bold tracking-tight font-mono">{{ summary.storage.assets_human }}</dd>
        </div>
        <div class="rounded-lg border p-4">
          <dt class="text-muted-foreground text-sm font-normal">上次备份</dt>
          <dd class="mt-1 text-base font-semibold">{{ backups.last_backup_at || "从未备份" }}</dd>
          <p class="text-muted-foreground mt-1 text-sm font-normal">本地保留最近 7 份历史快照</p>
        </div>
      </dl>

      <!-- Mobile: two equal-width buttons side by side (grid-cols-2); desktop: inline row. -->
      <div class="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap sm:items-center">
        <Button variant="outline" class="w-full sm:w-auto" icon="download" :loading="backingUp" @click="createBackup">
          立即备份
        </Button>
        <Button
          variant="outline"
          class="w-full sm:w-auto"
          icon="upload"
          disabled
          title="需求 8.2 节未定义从快照恢复的语义，暂不提供"
        >
          上传并恢复快照
        </Button>
      </div>

      <div>
        <h3 class="text-muted-foreground mb-2 text-sm font-normal">
          备份快照（{{ backups.items.length }} 份）
        </h3>
        <p v-if="backups.items.length === 0" class="text-muted-foreground m-0 text-sm">
          还没有备份，点「立即备份」生成第一份。
        </p>
        <ul v-else class="divide-border/60 divide-y">
          <li
            v-for="b in backups.items"
            :key="b.file_name"
            class="flex flex-wrap items-center gap-3 py-2.5 text-sm"
          >
            <span class="font-mono text-sm">{{ b.timestamp }}</span>
            <StatusBadge variant="secondary">{{ b.tier }}</StatusBadge>
            <span class="text-muted-foreground text-sm font-normal tabular-nums">{{ humanBytes(b.bytes) }}</span>
            <span class="text-muted-foreground text-sm font-normal">{{ b.integrity }}</span>
            <a
              :href="backupDownloadURL(b.file_name)"
              :download="b.file_name"
              class="text-foreground hover:bg-accent ml-auto inline-flex items-center gap-1 rounded-md px-2 py-1 text-sm transition-colors"
            >
              <Icon name="download" :size="14" />下载
            </a>
          </li>
        </ul>
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import Button from "../ui/Button.vue";
import ErrorState from "../ErrorState.vue";
import Icon from "../Icon.vue";
import StatusBadge from "../StatusBadge.vue";
import { backupDownloadURL, settingsApi, type BackupList } from "../../api/settings";
import { metricsApi } from "../../api/metrics";
import type { MetricsSummary } from "../../api/types";

/**
 * StorageSection — the "storage health & cold backup" section of the settings page (metrics + backup).
 *
 * All data comes from real endpoints (/metrics/summary + /admin/backups) — no number
 * is fabricated into the UI (better one fewer cell than a fake reading).
 */
const summary = ref<MetricsSummary | null>(null);
const backups = ref<BackupList>({ items: [], total: 0, last_backup_at: "", has_backup: false });
const loading = ref(false);
const backingUp = ref(false);
const error = ref("");

const diskPercent = computed(() => Math.round((summary.value?.storage.disk_used_ratio ?? 0) * 100));
const diskText = computed(() => {
  const s = summary.value?.storage;
  if (!s) return "—";
  if (!s.disk_supported) return "当前平台不支持";
  return `${Math.round(s.disk_used_ratio * 100)}%`;
});

function humanBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  const units = ["KB", "MB", "GB"];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(1)} ${units[i]}`;
}

async function loadAll(): Promise<void> {
  loading.value = true;
  error.value = "";
  try {
    const [m, b] = await Promise.all([metricsApi.summary(), settingsApi.backups()]);
    summary.value = m;
    backups.value = b;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    loading.value = false;
  }
}

async function createBackup(): Promise<void> {
  backingUp.value = true;
  error.value = "";
  try {
    await settingsApi.createBackup();
    backups.value = await settingsApi.backups();
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    backingUp.value = false;
  }
}

onMounted(loadAll);

// Exposed for the top-of-page "back up now" action — one backup code path.
defineExpose({ loadAll, createBackup });
</script>
