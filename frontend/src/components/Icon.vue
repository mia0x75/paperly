<template>
  <span
    class="material-symbols-rounded select-none leading-none"
    :style="style"
    aria-hidden="true"
    >{{ glyph }}</span
  >
</template>

<script setup lang="ts">
import { computed } from "vue";

/**
 * Icon -- self-hosted Material Symbols Rounded (subset on demand, addressed via PUA
 * codepoints).
 * Icon font lives in theme/tokens.css; the font is from Google Fonts, subset and inlined into the single binary.
 *
 * Name list (consistent with AGENTS.md "icon list"):
 *   navigation description/people/settings
 *   actions add/edit/delete/save/cancel/search/filter/refresh/download/upload
 *   status check_circle/error/warning/info/pending
 *   files image/picture_as_pdf/folder
 *   user person/person_add/visibility/visibility_off
 *   charts bar_chart/show_chart/pie_chart
 *   theme light_mode/dark_mode/brightness_auto
 *   generic menu/close/arrow_back/arrow_forward/chevron_left/chevron_right/home/logout/
 *        calendar_today/trending_up/trending_down/star/bookmark/notifications/help/
 *        open_in_new/more_vert/check/clear/expand_more/expand_less/expand/
 *        restore_from_trash/restore/settings_backup_restore/horizontal_rule
 */
export type IconName =
  | "description" | "people" | "settings"
  | "add" | "edit" | "delete" | "save" | "cancel" | "search" | "filter"
  | "refresh" | "download" | "upload"
  | "check_circle" | "error" | "warning" | "info" | "pending"
  | "image" | "picture_as_pdf" | "folder"
  | "person" | "person_add" | "visibility" | "visibility_off"
  | "bar_chart" | "show_chart" | "pie_chart"
  | "light_mode" | "dark_mode" | "brightness_auto"
  | "menu" | "close" | "arrow_back" | "arrow_forward" | "chevron_left" | "chevron_right"
  | "home" | "logout" | "calendar_today" | "trending_up" | "trending_down" | "star"
  | "bookmark" | "notifications" | "help" | "open_in_new" | "more_vert" | "check"
  | "clear" | "expand_more" | "expand_less" | "expand" | "horizontal_rule"
  | "restore_from_trash" | "restore" | "settings_backup_restore"
  | "format_bold" | "format_italic" | "format_underlined"
  | "format_list_bulleted" | "format_list_numbered"
  | "format_h1" | "format_h2" | "format_h3" | "format_h4"
  | "format_clear"
  | "undo" | "redo";

/** icon name -> Material Symbols Rounded PUA codepoint (the subset font addresses glyphs by this). */
const CODEPOINT: Record<IconName, number> = {
  description: 0xe873,
  people: 0xf233, // subset substitutes groups (people is ligature-only, no standalone codepoint)
  settings: 0xe8b8,
  add: 0xe145,
  edit: 0xe150,
  delete: 0xe872,
  save: 0xe161,
  cancel: 0xe5c9,
  search: 0xe8b6,
  filter: 0xe3b6,
  refresh: 0xe5d5,
  download: 0xe171,
  upload: 0xe2c6,
  check_circle: 0xe86c,
  error: 0xe000,
  warning: 0xe002,
  info: 0xe88e,
  pending: 0xef64,
  image: 0xe251,
  picture_as_pdf: 0xe415,
  folder: 0xe2c7,
  person: 0xe7fd,
  person_add: 0xe7fe,
  visibility: 0xe417,
  visibility_off: 0xe8f5,
  bar_chart: 0xe26b,
  show_chart: 0xe6e1,
  pie_chart: 0xe6c4,
  light_mode: 0xe518,
  dark_mode: 0xe51c,
  brightness_auto: 0xe1ab,
  menu: 0xe5d2,
  close: 0xe14c,
  arrow_back: 0xe5c4,
  arrow_forward: 0xe5c8,
  chevron_left: 0xe408,
  chevron_right: 0xe409,
  home: 0xe88a,
  logout: 0xe9ba,
  calendar_today: 0xe935,
  trending_up: 0xe8e5,
  trending_down: 0xe8e3,
  star: 0xe838,
  bookmark: 0xe866,
  notifications: 0xe7f4,
  help: 0xe887,
  open_in_new: 0xe895,
  more_vert: 0xe5d4,
  check: 0xe5ca,
  clear: 0xe5c9,
  expand_more: 0xe5cf,
  expand_less: 0xe5ce,
  horizontal_rule: 0xf108,
  expand: 0xe94f,
  restore_from_trash: 0xe938,
  restore: 0xe8b3,
  settings_backup_restore: 0xe8ba,
  format_bold: 0xe238,
  format_italic: 0xe23f,
  format_underlined: 0xe249,
  format_list_bulleted: 0xe241,
  format_list_numbered: 0xe242,
  format_h1: 0xf85d,
  format_h2: 0xf85e,
  format_h3: 0xf85f,
  format_h4: 0xf860,
  format_clear: 0xe239,
  undo: 0xe166,
  redo: 0xe15a,
};

const props = withDefaults(
  defineProps<{
    name: IconName;
    size?: number;
    /** fill: 0 outlined / 1 filled (subset font is static; API retained). */
    fill?: number;
    weight?: number;
  }>(),
  { size: 20, fill: 0, weight: 400 },
);

const glyph = computed(() => String.fromCodePoint(CODEPOINT[props.name]));

const style = computed(() => ({
  fontSize: `${props.size}px`,
  width: `${props.size}px`,
  height: `${props.size}px`,
  fontVariationSettings: `"FILL" ${props.fill}, "wght" ${props.weight}`,
}));
</script>

<!--
  No <style> block here: Bun's SFC plugin compiles script + template only and silently
  drops component styles (see scripts/vue-plugin.ts). The font rule lives in src/styles/app.css.
-->
