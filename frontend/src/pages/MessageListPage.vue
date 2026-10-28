<template>
  <div class="flex flex-col gap-6">
    <section class="bg-card text-card-foreground flex flex-col gap-4 rounded-xl border p-6 shadow-sm">
      <!-- Header row: "全部已读" (only when something is unread) + the page's own title.
           No filters on purpose — a notification inbox is chronological, read state is
           carried by the dot, not by a filter row. -->
      <div class="flex flex-wrap items-center justify-between gap-2">
        <h1 class="text-base font-semibold tracking-tight text-foreground">通知</h1>
        <Button
          v-if="hasUnread"
          variant="outline"
          size="sm"
          :loading="markingAll"
          @click="onMarkAllRead"
        >
          全部已读
        </Button>
      </div>

      <ErrorState v-if="error !== ''" :message="error" retry-label="重试" @retry="load" />

      <!-- Table sits directly on the section card (same as the exam list): the section is
           already a solid bg-card, so no extra card wrapper is needed. -->
      <template v-if="loading || items.length > 0">
        <Table :columns="columns" :data="rows">
          <!-- Read state rides on a dot before the title (unread = solid red, read = light
               gray), exactly like the exam list's status dot: the meaning travels with
               the title, so no separate status column is needed. -->
          <template #cell-title="{ row }">
            <span class="inline-flex items-center gap-2">
              <LegendChip
                bare
                :dot-class="row.is_read === true ? 'bg-muted-foreground/40' : 'bg-destructive'"
                :title="row.is_read === true ? '已读' : '未读'"
                :aria-label="row.is_read === true ? '已读' : '未读'"
                role="img"
              />
              <!-- A notification carrying a link is a deep link (REQUIREMENTS 4.13):
                   the title jumps straight to the target. Reading it also marks it read. -->
              <RouterLink
                v-if="row.link !== '' && row.link !== null"
                :to="String(row.link)"
                class="text-foreground hover:text-foreground font-medium transition-colors no-underline"
                @click="onTitleClick(row)"
              >
                {{ row.title }}
              </RouterLink>
              <button
                v-else
                type="button"
                class="text-foreground hover:text-foreground font-medium transition-colors"
                @click="onTitleClick(row)"
              >
                {{ row.title }}
              </button>
            </span>
          </template>
          <template #cell-kind="{ row }">
            {{ kindLabel(Number(row.kind)) }}
          </template>
          <template #cell-created_at="{ row }">
            <span class="text-xs font-normal">{{ formatRelative(String(row.created_at)) }}</span>
          </template>
          <template #cell-actions="{ row }">
            <Button
              variant="ghost"
              size="icon"
              icon="delete"
              title="删除通知"
              :aria-label="`删除通知：${String(row.title)}`"
              class="size-8"
              @click="askDelete(Number(row.id), String(row.title))"
            />
          </template>
        </Table>
        <!-- Mobile: persistent accent chips; desktop: transparent until hover. -->
        <div class="px-6 text-xs text-muted-foreground flex items-center gap-4 justify-center">
          <LegendChip dot-class="bg-destructive">未读</LegendChip>
          <LegendChip dot-class="bg-muted-foreground/40">已读</LegendChip>
        </div>
      </template>
      <EmptyState
        v-else-if="!error"
        icon="notifications"
        title="暂无通知"
        description="备份、年级变更等提醒会出现在这里。"
      />

      <div class="flex flex-wrap items-center justify-between gap-2">
        <span class="text-muted-foreground text-sm">
          <template v-if="loading"> · 加载中…</template>
        </span>
        <div class="flex items-center gap-2">
          <Button variant="outline" size="sm" :disabled="pageNo <= 1 || loading" @click="prevPage">
            上一页
          </Button>
          <Button variant="outline" size="sm" :disabled="!hasMore || loading" @click="nextPage">
            下一页
          </Button>
        </div>
      </div>
    </section>

    <ConfirmDialog
      :show="deleteTarget !== null"
      title="删除通知"
      :message="`确定删除「${deleteTarget?.title ?? ''}」？删除后不可恢复。`"
      :loading="deleting"
      @confirm="confirmDelete"
      @cancel="deleteTarget = null"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import Button from "../components/ui/Button.vue";
import ConfirmDialog from "../components/ConfirmDialog.vue";
import EmptyState from "../components/EmptyState.vue";
import ErrorState from "../components/ErrorState.vue";
import LegendChip from "../components/LegendChip.vue";
import Table, { type TableColumn } from "../components/Table.vue";
import { messageApi } from "../api/message";
import { formatRelative, kindLabel } from "../lib/message";
import type { MessageListResponse } from "../api/types";

/**
 * MessageListPage: the full notification list (`/messages`, `/kid/messages`).
 *
 * Same page component on both sides (the kid side reuses the parent component, prefix
 * only — see src/router/index.ts); the recipient boundary is decided by the token
 * audience server-side, so the client never passes kid_id.
 *
 * Deliberately without filters: notifications are short and few, the list is a
 * chronological inbox — the read state rides on the dot, not on a filter row.
 */
const PAGE_SIZE = 10;

const data = ref<MessageListResponse | null>(null);
const loading = ref(false);
const error = ref("");

/** Cursor pagination: cursorStack[0] is always 0; a stack (not just next_cursor)
 *  supports "previous page" (keyset can't go back). */
const cursorStack = ref<number[]>([0]);
const hasMore = ref(false);

/** The notification pending deletion (null = no dialog). */
const deleteTarget = ref<{ id: number; title: string } | null>(null);
const deleting = ref(false);
const markingAll = ref(false);

const items = computed(() => data.value?.items ?? []);
/** Display-only page number (not a query param). */
const pageNo = computed(() => cursorStack.value.length);
const hasUnread = computed(() => items.value.some((m) => !m.is_read));

// Table's generic needs Record<string, unknown>; MessageSummary has none — convert here,
// don't touch the domain contract.
const rows = computed<Record<string, unknown>[]>(
  () => items.value as unknown as Record<string, unknown>[],
);

const columns: TableColumn[] = [
  { key: "title", title: "通知" },
  { key: "kind", title: "类型", class: "w-24" },
  { key: "created_at", title: "时间", class: "w-32" },
  { key: "actions", title: "操作", class: "w-16" },
];

/** Load one page with the keyset cursor; cursor 0 = newest first, so it is not sent. */
async function load(): Promise<void> {
  loading.value = true;
  error.value = "";
  try {
    const cursor = cursorStack.value[cursorStack.value.length - 1] ?? 0;
    const params = {
      limit: PAGE_SIZE,
      ...(cursor > 0 ? { cursor } : {}),
    };
    data.value = await messageApi.list(params);
    hasMore.value = data.value.has_more;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
    data.value = null;
  } finally {
    loading.value = false;
  }
}

/** Next page: push this page's next_cursor onto the stack. */
function nextPage(): void {
  const next = data.value?.next_cursor ?? 0;
  if (next === 0) return;
  cursorStack.value = [...cursorStack.value, next];
  void load();
}

/** Previous page: pop the stack. */
function prevPage(): void {
  if (cursorStack.value.length <= 1) return;
  cursorStack.value = cursorStack.value.slice(0, -1);
  void load();
}

/**
 * Reading a notification marks it read (the dot turns gray in place). The deep link
 * itself is the title's job — this only flips the state.
 */
async function onTitleClick(row: Record<string, unknown>): Promise<void> {
  const id = Number(row.id);
  const m = items.value.find((x) => x.id === id);
  if (m === undefined || m.is_read) return;
  m.is_read = true;
  try {
    await messageApi.markRead(id);
  } catch {
    // A failed mark self-corrects on the next reload.
    void load();
  }
}

/** Mark every notification read (same endpoint as the bell's "全部已读"). */
async function onMarkAllRead(): Promise<void> {
  markingAll.value = true;
  try {
    await messageApi.markAllRead();
    for (const m of items.value) m.is_read = true;
    await load();
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    markingAll.value = false;
  }
}

function askDelete(id: number, title: string): void {
  deleteTarget.value = { id, title };
}

async function confirmDelete(): Promise<void> {
  const target = deleteTarget.value;
  if (target === null) return;
  deleting.value = true;
  try {
    await messageApi.remove(target.id);
    deleteTarget.value = null;
    await reloadAfterDelete();
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
    deleteTarget.value = null;
  } finally {
    deleting.value = false;
  }
}

/**
 * Reload the current page after a delete. When the page became empty (the last row
 * was the one deleted), step back one page instead of showing an empty table.
 */
async function reloadAfterDelete(): Promise<void> {
  await load();
  if (items.value.length === 0 && cursorStack.value.length > 1) {
    cursorStack.value = cursorStack.value.slice(0, -1);
    await load();
  }
}

onMounted(() => {
  void load();
});

// Exposed for page-level tests: read-only actions only, never internal refs.
defineExpose({ load });
</script>
