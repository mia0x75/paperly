<template>
  <div class="flex min-h-dvh flex-col">
    <main class="container mx-auto flex w-full max-w-5xl flex-1 flex-col px-4 py-4 sm:py-4">
      <header class="mb-4 flex items-center justify-between gap-3 sm:mb-6">
        <div class="flex min-w-0 items-end gap-2.5">
          <!-- Brand entry: desktop shows the big Jersey logo; on mobile a home icon button
               (same spec as the action-row buttons) replaces it, so a long kid name never
               squeezes the action row. -->
          <Logo :to="isKid ? '/kid' : '/'" class="hidden sm:inline-block" />
          <RouterLink
            :to="isKid ? '/kid' : '/'"
            title="首页"
            aria-label="首页"
            class="border-border/50 bg-control text-muted-foreground hover:bg-accent hover:text-accent-foreground inline-flex size-12 items-center justify-center rounded-lg border transition-all duration-150 ease-out active:scale-[0.98] sm:hidden"
          >
            <Icon name="home" :size="22" />
          </RouterLink>
          <!-- Kid top bar shows "{name}'s learning space": the name is only known from the
               home page's dashboard query, stored in kidSession, and read reactively here so
               it survives a refresh on any kid page. truncate+min-w-0 keep a long name from
               compressing the action row. -->
          <span
            v-if="isKid && kidSession.name"
            class="text-foreground min-w-0 truncate text-sm font-medium"
          >
            {{ kidSession.name }}的学习空间
          </span>
          <!-- Parent side shows the brand wordmark next to the logo (tablet/desktop);
               hidden on mobile where the big logo is replaced by the home icon. -->
          <span v-else class="text-foreground hidden text-sm font-medium sm:inline-block">
            拾卷
          </span>
        </div>

        <!-- Right action area: quick-add exam + exams + bell + settings, on one row.
             shrink-0 keeps buttons at full touch size even with a long left title; size-12
             on mobile (48px), size-9 from sm up. Tight mobile gap gives the buttons more room. -->
        <nav class="flex shrink-0 items-center gap-1 sm:gap-2.5">
          <!-- Quick-add exam: exams are added often, so give it a distinct prominent entry. -->
          <RouterLink
            :to="uploadTo"
            title="添加试卷"
            class="border-border/50 bg-control text-muted-foreground hover:bg-accent hover:text-accent-foreground inline-flex size-12 items-center justify-center rounded-lg border transition-all duration-150 ease-out active:scale-[0.98] sm:size-9"
          >
            <Icon name="add" :size="22" />
          </RouterLink>

          <RouterLink
            :to="examsTo"
            title="试卷"
            class="border-border/50 bg-control text-muted-foreground hover:bg-accent hover:text-accent-foreground inline-flex size-12 items-center justify-center rounded-lg border transition-all duration-150 ease-out active:scale-[0.98] sm:size-9"
            active-class="!text-foreground"
          >
            <Icon name="description" :size="22" />
          </RouterLink>

          <SettingsMenu :items="isKid ? KID_MENU : undefined" />

          <MessageBell />

          <!-- Theme toggle lives in the top bar: it's an adjust-anytime preference, not config
               that belongs inside settings. Same spec as the other buttons. -->
          <button
            type="button"
            :title="modeLabel"
            :aria-label="modeLabel"
            class="border-border/50 bg-control text-muted-foreground hover:bg-accent hover:text-accent-foreground inline-flex size-12 items-center justify-center rounded-lg border transition-all duration-150 ease-out active:scale-[0.98] sm:size-9"
            @click="toggle"
          >
            <Icon :name="modeIcon" :size="22" />
          </button>
        </nav>
      </header>

      <p v-if="kidError" class="text-destructive mb-4 text-sm">{{ kidError }}</p>

      <div class="flex flex-1 flex-col">
        <slot />
      </div>

      <Footer />
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from "vue";
import { RouterLink } from "vue-router";
import { storeToRefs } from "pinia";
import Icon, { type IconName } from "../components/Icon.vue";
import Logo from "../components/Logo.vue";
import MessageBell from "../components/MessageBell.vue";
import SettingsMenu, { type SettingsMenuItem } from "../components/SettingsMenu.vue";
import Footer from "../components/Footer.vue";
import { useThemeMode } from "../theme/mode";
import { useKidStore } from "../stores/kid";
import { useKidSessionStore } from "../stores/kidSession";

/**
 * Layout — page layout: main area max-w-5xl, header with LEFT LOGO / RIGHT actions, no sidebar.
 *
 * The top bar is one action chain + one persistent preference:
 *   exams → quick-add exam → bell → settings, ending with the theme toggle.
 * The theme toggle lives in the top bar: it's an adjust-anytime display preference,
 * not something hidden inside a settings page.
 *
 * variant decides whose perspective the top bar renders under (passed explicitly by
 * ParentLayout / KidLayout, not inferred from a path prefix — an implicit convention
 * dies when the path changes and can't be unit-tested):
 *   parent (default) — exams goes to /exams, quick-add exam, 6-item settings menu.
 *   kid — kid side. exams goes to /kid/exams, bell, theme, Logo, Footer retained.
 */
const props = withDefaults(defineProps<{ variant?: "parent" | "kid" }>(), {
  variant: "parent",
});

const isKid = computed(() => props.variant === "kid");

/** Kid variant's settings menu: only subject / knowledge-point management open to the kid side. */
const KID_MENU: SettingsMenuItem[] = [
  { path: "/kid/subjects", label: "科目管理", icon: "description" },
  { path: "/kid/knowledge-points", label: "知识点管理", icon: "bookmark" },
];

/** Kid-side exam entry: literal /kid/exams, always present (the entry page already
 *  captured the access code into localStorage). */
const examsTo = computed(() => (isKid.value ? "/kid/exams" : "/exams"));

/** Add-exam entry: /exams/upload (parent) or /kid/exams/upload (kid). */
const uploadTo = computed(() => (isKid.value ? "/kid/exams/upload" : "/exams/upload"));

const { resolved, toggle } = useThemeMode();
const modeIcon = computed<IconName>(() => (resolved.value === "dark" ? "dark_mode" : "light_mode"));
const modeLabel = computed(() => (resolved.value === "dark" ? "切换到亮色" : "切换到暗色"));

const kidStore = useKidStore();
const { error: kidError } = storeToRefs(kidStore);

const kidSession = useKidSessionStore();

onMounted(() => {
  // Parent side prefetches the kid list for other pages; the kid variant skips it to
  // save one /api/kids call.
  if (!isKid.value && kidStore.list.length === 0) {
    void kidStore.fetchList();
  }
});
</script>
