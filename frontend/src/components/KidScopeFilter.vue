<template>
  <DropdownMenu :trigger-class="TRIGGER" panel-class="w-48" :title="`筛选：${label}`">
    <template #trigger>
      <span class="max-w-24 truncate">{{ label }}</span>
      <Icon name="expand_more" :size="16" class="text-muted-foreground shrink-0" />
    </template>

    <button
      v-for="item in options"
      :key="item.value ?? 'all'"
      type="button"
      role="menuitem"
      :data-scope-id="item.value ?? 'all'"
      class="hover:bg-accent hover:text-accent-foreground flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left text-sm transition-colors"
      @click="pick(item.value)"
    >
      <span class="flex-1 truncate">{{ item.label }}</span>
      <!-- Checkmark slot always reserved: appearing only on select would shift option text. -->
      <Icon
        v-if="item.value === modelValue"
        name="check"
        :size="16"
        class="text-foreground shrink-0"
      />
      <span v-else class="w-4 shrink-0" aria-hidden="true" />
    </button>
  </DropdownMenu>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { storeToRefs } from "pinia";
import DropdownMenu from "./ui/DropdownMenu.vue";
import Icon from "./Icon.vue";
import { useKidStore } from "../stores/kid";
import { KID_STATUS } from "../api/types";

/**
 * KidScopeFilter -- card-level "all / one kid" filter (ghost trigger + dropdown).
 *
 * Defaults to "all" unless `allowAll=false`. Never calls kidStore.select — the
 * filter is a local view and must not move the top-bar "current kid".
 * Lists only active kids.
 */
const props = withDefaults(
  defineProps<{
    modelValue: number | null;
    /** Offer the "全部" option (radar passes false: its axes need one kid's kid_subjects). */
    allowAll?: boolean;
  }>(),
  { allowAll: true },
);
const emit = defineEmits<{ "update:modelValue": [id: number | null] }>();

const { list } = storeToRefs(useKidStore());

const activeKids = computed(() => list.value.filter((k) => k.status === KID_STATUS.ACTIVE));

type Option = { value: number | null; label: string };

const options = computed<Option[]>(() => {
  const kids = activeKids.value.map((k) => ({ value: k.id as number | null, label: k.name }));
  return props.allowAll ? [{ value: null, label: "全部" }, ...kids] : kids;
});

const label = computed(() => {
  const found = options.value.find((o) => o.value === props.modelValue);
  if (found) return found.label;
  if (!props.allowAll) return options.value[0]?.label ?? "";
  return "全部";
});

function pick(value: number | null): void {
  emit("update:modelValue", value);
}

/**
 * Ghost trigger: h-8 matches the title row; border/bg-transparent override DropdownMenu's
 * solid defaults. hover:bg-transparent is required too — without it tailwind-merge sees
 * no conflict and the accent hover survives. No horizontal padding: the row's px-4
 * already positions it flush right.
 */
const TRIGGER =
  "h-8 gap-1 border-transparent bg-transparent text-muted-foreground " +
  "hover:bg-transparent hover:text-accent-foreground";
</script>
