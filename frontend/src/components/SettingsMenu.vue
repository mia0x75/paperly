<template>
  <DropdownMenu
    :trigger-class="TRIGGER"
    panel-class="w-64"
    panel-align="viewport"
    title="系统设置"
  >
    <template #trigger>
      <Icon name="settings" :size="22" class="text-muted-foreground" />
    </template>

    <router-link
      v-for="item in entries"
      :key="item.path"
      :to="item.path"
      role="menuitem"
      :data-menu-path="item.path"
      class="hover:bg-accent hover:text-accent-foreground flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left text-sm transition-colors"
    >
      <Icon :name="item.icon" :size="16" class="text-muted-foreground shrink-0" />
      <span class="flex-1 truncate">{{ item.label }}</span>
    </router-link>
  </DropdownMenu>
</template>

<script setup lang="ts">
import { computed } from "vue";
import DropdownMenu from "./ui/DropdownMenu.vue";
import Icon, { type IconName } from "./Icon.vue";

/** Menu item: icon + label + target path. */
export interface SettingsMenuItem {
  path: string;
  label: string;
  icon: IconName;
}

/**
 * SettingsMenu -- the top-bar "settings" entry: a pure navigation menu, each item
 * a separate top-level route (no forms inside).
 *
 * Close timing (click outside / Esc / click item) comes from DropdownMenu. Passing
 * `items` only swaps entries — popover behavior never changes (the kid side reuses
 * this with a shorter menu). Defaults to the 8 parent-side MENU items.
 */
const props = defineProps<{ items?: SettingsMenuItem[] }>();

const TRIGGER =
  "border-border/50 bg-control text-muted-foreground hover:bg-accent hover:text-accent-foreground inline-flex size-12 items-center justify-center rounded-lg border transition-all duration-150 ease-out active:scale-[0.98] sm:size-9";

/** Item order matches the requirements list; icons only from the in-repo subset. */
const MENU: SettingsMenuItem[] = [
  { path: "/storage", label: "存储健康与冷备份", icon: "folder" },
  { path: "/image", label: "图片压缩", icon: "image" },
  { path: "/kids-auth", label: "孩子与免密授权", icon: "people" },
  { path: "/parents", label: "家长管理", icon: "person" },
  { path: "/tasks", label: "异步与后台队列", icon: "refresh" },
  { path: "/subjects", label: "科目管理", icon: "description" },
  { path: "/knowledge-points", label: "知识点管理", icon: "bookmark" },
  { path: "/system", label: "系统版本迁移与任务游标", icon: "settings" },
];

const entries = computed<SettingsMenuItem[]>(() => props.items ?? MENU);
</script>
