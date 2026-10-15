<template>
  <CalendarRoot
    v-slot="{ grid, weekDays, date: placeholder }"
    v-model="value"
    :week-starts-on="1"
    :min-value="minValue"
    :max-value="maxValue"
    locale="zh-CN"
    fixed-weeks
  >
    <!-- data-placeholder exposes the currently displayed month for test targeting/paging.
         It must be bound on the inner element: a slot variable is not accessible
         from attributes of the element that binds it (Vue warns
         "placeholder accessed during render"). -->
    <div class="p-3" :data-placeholder="placeholder ? placeholder.toString() : ''">
      <CalendarHeader class="mb-2 flex items-center justify-between">
        <CalendarPrev
          class="hover:bg-accent hover:text-accent-foreground focus-visible:ring-ring flex size-11 items-center justify-center rounded-md sm:size-7 transition-all duration-150 ease-out outline-none focus-visible:ring-2 disabled:pointer-events-none disabled:opacity-40"
          aria-label="上个月"
        >
          <Icon name="chevron_left" :size="16" />
        </CalendarPrev>
        <CalendarHeading class="text-sm font-medium" />
        <CalendarNext
          class="hover:bg-accent hover:text-accent-foreground focus-visible:ring-ring flex size-11 items-center justify-center rounded-md sm:size-7 transition-all duration-150 ease-out outline-none focus-visible:ring-2 disabled:pointer-events-none disabled:opacity-40"
          aria-label="下个月"
        >
          <Icon name="chevron_right" :size="16" />
        </CalendarNext>
      </CalendarHeader>

      <CalendarGrid v-for="month in grid" :key="month.value.toString()" class="w-full border-collapse">
        <CalendarGridHead>
          <CalendarGridRow class="flex">
            <CalendarHeadCell
              v-for="day in weekDays"
              :key="day"
              class="text-muted-foreground w-8 text-center text-xs font-normal"
            >
              {{ day }}
            </CalendarHeadCell>
          </CalendarGridRow>
        </CalendarGridHead>
        <CalendarGridBody>
          <CalendarGridRow v-for="(weekDates, wi) in month.rows" :key="`w-${wi}`" class="mt-1 flex">
            <CalendarCell v-for="date in weekDates" :key="date.toString()" :date="date" class="p-0">
              <CalendarCellTrigger
                :day="date"
                :month="month.value"
                :data-date="date.toString()"
                :class="CELL_CLASS"
              />
            </CalendarCell>
          </CalendarGridRow>
        </CalendarGridBody>
      </CalendarGrid>
    </div>
  </CalendarRoot>
</template>

<script setup lang="ts">
import {
  CalendarCell,
  CalendarCellTrigger,
  CalendarGrid,
  CalendarGridBody,
  CalendarGridHead,
  CalendarGridRow,
  CalendarHeadCell,
  CalendarHeader,
  CalendarHeading,
  CalendarNext,
  CalendarPrev,
  CalendarRoot,
} from "radix-vue";
import type { DateValue } from "@internationalized/date";
import { computed } from "vue";
import Icon from "../Icon.vue";
import { cn } from "../../lib/utils";

/**
 * Calendar: a themed month picker on radix-vue's Calendar primitive.
 *
 * Not hand-rolled: leap months, cross-month padding, week-start, keyboard nav and
 * today/selected states are a pile of edge cases. Only zinc-theming here.
 * v-model is DateValue (@internationalized/date); string callers use DatePicker.
 */
const props = withDefaults(
  defineProps<{
    modelValue?: DateValue;
    /** Readable min/max dates (inclusive) bounding the selectable range. */
    minValue?: DateValue;
    maxValue?: DateValue;
  }>(),
  {},
);

const emit = defineEmits<{ "update:modelValue": [value: DateValue] }>();

const value = computed({
  get: () => props.modelValue,
  set: (v: DateValue | undefined) => {
    if (v) emit("update:modelValue", v);
  },
});

/**
 * Cell states via data-* selectors (radix annotates data-selected/today/outside-view/disabled).
 */
const CELL_CLASS = cn(
  "flex size-8 items-center justify-center rounded-md text-sm transition-all duration-150 ease-out",
  "hover:bg-accent hover:text-accent-foreground",
  "focus-visible:border-ring focus-visible:ring-ring outline-none focus-visible:ring-2",
  "data-[today]:bg-accent data-[today]:text-accent-foreground data-[today]:font-medium",
  "data-[selected]:bg-primary data-[selected]:text-primary-foreground data-[selected]:hover:bg-primary",
  "data-[outside-view]:text-muted-foreground data-[outside-view]:opacity-50",
  "data-[disabled]:pointer-events-none data-[disabled]:opacity-40",
  "data-[unavailable]:pointer-events-none data-[unavailable]:line-through data-[unavailable]:opacity-40",
);
</script>
