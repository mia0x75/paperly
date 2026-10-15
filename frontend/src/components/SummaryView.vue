<template>
  <div class="flex flex-col gap-4">
    <p v-if="segments.length === 0" class="text-muted-foreground flex flex-col items-center gap-2 py-6 text-center text-sm">
      <Icon name="description" :size="28" class="opacity-40" />
      还没有总结，追加一条记录这次的经验吧。
    </p>

    <div v-for="(seg, i) in segments" :key="seg.sig || `unmarked-${i}`" class="summary-segment">
      <div v-if="seg.hasSign" class="mb-1 flex items-center justify-between gap-2">
        <span class="text-muted-foreground text-sm font-normal">{{ seg.signText }}</span>
        <button
          v-if="canDelete(seg)"
          type="button"
          class="text-muted-foreground hover:text-destructive transition-all duration-150 ease-out disabled:opacity-40"
          :disabled="deletingSig === seg.sig"
          aria-label="删除该条总结"
          @click="emit('delete', seg.sig)"
        >
          <Icon name="delete" :size="16" />
        </button>
      </div>
      <!-- eslint-disable-next-line vue/no-v-html -->
      <div class="summary-content" v-html="seg.bodyHtml" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import Icon from "./Icon.vue";
import { parseSummary, canDeleteSummarySegment, type SummarySegment } from "../lib/summary";

/**
 * SummaryView -- renders the append-only signed summary read-only.
 *
 * The body HTML is sanitized by parseSummary (whitelist sanitizer) before v-html;
 * never render the raw stored summary here.
 */
const props = defineProps<{
  summary: string;
  isKid: boolean;
  selfKidId: number | null;
  deletingSig?: string;
}>();

const emit = defineEmits<{ delete: [sig: string] }>();

const segments = computed<SummarySegment[]>(() => parseSummary(props.summary));

function canDelete(seg: SummarySegment): boolean {
  return canDeleteSummarySegment(seg, { isKid: props.isKid, selfKidId: props.selfKidId });
}
</script>
