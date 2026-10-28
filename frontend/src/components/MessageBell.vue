<template>
  <DropdownMenu
    :trigger-class="TRIGGER"
    panel-class="w-80"
    panel-align="viewport"
    :title="unreadCount > 0 ? `${unreadCount} 条未读通知` : '通知'"
    @update:open="onOpenChange"
  >
    <template #trigger>
      <Icon name="notifications" :size="22" />
      <!-- Badge only indicates "has / has not": a single red dot, no number.
            A number would force mental arithmetic of "how many left", while the bell's job is only to
            signal "something new"; the real count is in the list after opening. ring-background keeps
            the red dot legible on any background. -->
      <span
        v-if="unreadCount > 0"
        data-testid="unread-badge"
        class="bg-destructive ring-background absolute -top-0.5 -right-0.5 size-2 rounded-full ring-2"
      />
    </template>

    <div class="flex items-center justify-between px-3 pt-2 pb-1">
      <span class="text-sm font-semibold tracking-tight">通知</span>
      <button
        v-if="unreadCount > 0"
        type="button"
        class="text-muted-foreground hover:text-foreground text-xs transition-colors disabled:opacity-50"
        :disabled="markingAll"
        @click.stop="onMarkAllRead"
      >
        {{ markingAll ? "处理中…" : "全部已读" }}
      </button>
    </div>

    <!-- Loading skeleton matches the final row height, avoiding a whole-block jump when content lands -->
    <div v-if="loading" class="space-y-1 p-1">
      <div v-for="i in 3" :key="i" class="bg-muted animate-pulse h-11 rounded-lg" />
    </div>

    <div v-else-if="error !== ''" class="flex flex-col items-center gap-2 px-3 py-6 text-center">
      <Icon name="error" :size="24" class="text-destructive opacity-60" />
      <p class="text-muted-foreground m-0 text-xs">{{ error }}</p>
      <Button variant="outline" size="sm" icon="refresh" @click="load">重试</Button>
    </div>

    <div v-else-if="items.length === 0" class="flex flex-col items-center gap-2 px-3 py-6 text-center">
      <Icon name="notifications" :size="24" class="text-muted-foreground opacity-40" />
      <p class="m-0 text-sm font-medium">暂无通知</p>
      <p class="text-muted-foreground m-0 text-xs">备份、归档、复习提醒会出现在这里</p>
    </div>

    <template v-else>
      <!-- .stop: the panel closes on any panel click, but reading a notification is
           not a dismissal — the dropdown stays open so the dot visibly turns gray. -->
      <button
        v-for="m in items"
        :key="m.id"
        type="button"
        role="menuitem"
        class="flex w-full items-start gap-2.5 rounded-lg px-3 py-2 text-left transition-colors"
        :class="m.is_read ? 'opacity-60' : 'hover:bg-accent hover:text-accent-foreground'"
        @click.stop="onOpenMessage(m)"
      >
        <!-- Unread = solid dot, read = light gray dot: the item stays in place after
             being read (the gray dot is the only change), so the bell keeps showing
             the same six latest notifications instead of reshuffling on every click. -->
        <span
          class="mt-1.5 size-1.5 shrink-0 rounded-full"
          :class="m.is_read ? 'bg-muted-foreground/40' : 'bg-destructive'"
          aria-hidden="true"
        />
        <span class="min-w-0 flex-1">
          <span class="block truncate text-sm font-medium">{{ m.title }}</span>
          <span class="text-muted-foreground mt-0.5 block text-xs font-normal">
            {{ kindLabel(m.kind) }} · {{ formatRelative(m.created_at) }}
          </span>
        </span>
      </button>

      <!-- More than six notifications: the dropdown stays a glance, the full list
           lives on its own page (deleting / paging through notifications needs
           more room than a dropdown can offer). -->
      <div v-if="hasMore" class="border-t border-border/50 pt-1">
        <button
          type="button"
          class="text-muted-foreground hover:bg-accent hover:text-accent-foreground flex w-full items-center justify-center gap-1 rounded-lg px-3 py-2 text-xs transition-colors"
          @click="goToAll"
        >
          <Icon name="expand_more" :size="14" />全部通知
        </button>
      </div>
    </template>
  </DropdownMenu>
</template>

<script setup lang="ts">
import { inject, onMounted, ref } from "vue";
import { routerKey } from "vue-router";
import DropdownMenu from "./ui/DropdownMenu.vue";
import Button from "./ui/Button.vue";
import Icon from "./Icon.vue";
import { messageApi } from "../api/message";
import type { MessageSummary } from "../api/types";
import { formatRelative, kindLabel } from "../lib/message";

/**
 * TRIGGER -- same classes as the adjacent top-bar buttons. The DropdownMenu base defaults
 * to text-control-foreground, one notch brighter than its neighbors — override the whole
 * thing so cn() merges it to match Layout / SettingsMenu class by class.
 */
const TRIGGER =
  "border-border/50 bg-control text-muted-foreground hover:bg-accent hover:text-accent-foreground inline-flex size-12 items-center justify-center rounded-lg border transition-all duration-150 ease-out active:scale-[0.98] sm:size-9";

/**
 * MessageBell -- top-bar bell: red dot badge + dropdown notification list.
 *
 * Four design trade-offs:
 *   1. Badge and list fetch separately: every page shows the badge; pulling 50 records
 *      for 2 characters isn't worth it;
 *   2. The list loads only on first open (fetching on mount would fire on every page);
 *   3. The dropdown shows only the latest BELL_LIMIT notifications — it is a glance,
 *      not an inbox. More than that goes to the full list page ("全部通知");
 *   4. Clicking an item = mark read (the dot turns gray in place): the notification
 *      stays where it was, so reading one doesn't reshuffle the list under the cursor.
 */
const BELL_LIMIT = 6;

/** Target of the "全部通知" entry; the parent / kid variant points at its own list page. */
const props = withDefaults(defineProps<{ allTo?: string }>(), { allTo: "/messages" });

/** Router is optional (page-level tests mount without one): inject(routerKey, null)
 *  avoids useRouter()'s "injection not found" warning. */
const router = inject(routerKey, null);

const items = ref<MessageSummary[]>([]);
const unreadCount = ref(0);
const hasMore = ref(false);
const loading = ref(false);
const markingAll = ref(false);
const error = ref("");
let loaded = false;

async function load(): Promise<void> {
  loading.value = true;
  error.value = "";
  try {
    const res = await messageApi.list({ limit: BELL_LIMIT });
    items.value = res.items;
    hasMore.value = res.has_more;
    loaded = true;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    loading.value = false;
  }
}

/** Badge refresh stays silent on failure: it's decorative, never an error bar. */
async function refreshCount(): Promise<void> {
  try {
    unreadCount.value = await messageApi.unreadCount();
  } catch {
    // Keep the last value.
  }
}

function onOpenChange(open: boolean): void {
  if (open && !loaded) void load();
  if (open) void refreshCount();
}

/**
 * Click = mark read. The item stays in the list (gray dot instead of solid one) and
 * nothing else happens: the dropdown is a triage surface, the deep link belongs to
 * the full list page.
 */
async function onOpenMessage(m: MessageSummary): Promise<void> {
  if (m.is_read) return;
  // Optimistic: the dot turns gray immediately, the request only confirms it.
  m.is_read = true;
  unreadCount.value = Math.max(0, unreadCount.value - 1);
  try {
    await messageApi.markRead(m.id);
  } catch {
    // A failed mark self-corrects on the next refreshCount.
    void refreshCount();
  }
}

/** Jump to the full notification list (the dropdown itself closes on panel click). */
function goToAll(): void {
  void router?.push(props.allTo);
}

async function onMarkAllRead(): Promise<void> {
  markingAll.value = true;
  try {
    await messageApi.markAllRead();
    unreadCount.value = 0;
    for (const m of items.value) m.is_read = true;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    markingAll.value = false;
  }
}

onMounted(() => {
  void refreshCount();
});

// Exposed for page-level tests (microtask awaiting): read-only actions only, never internal refs.
defineExpose({ refreshCount, load });
</script>
