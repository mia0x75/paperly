<template>
  <div class="w-full overflow-x-auto">
    <table class="w-full caption-bottom text-sm">
      <thead>
        <tr class="border-b">
          <th
            v-for="col in columns"
            :key="col.key"
            class="text-muted-foreground h-10 px-2 text-left align-middle font-medium whitespace-nowrap"
            :class="col.class"
          >
            {{ col.title }}
          </th>
        </tr>
      </thead>
      <tbody>
        <!-- Index key is safe here: rows arrive pre-ordered from the backend and no
             caller re-sorts client-side (filters refetch); Table takes no rowKey prop. -->
        <tr
          v-for="(row, rowIndex) in data"
          :key="rowIndex"
          class="hover:bg-muted/30 border-b transition-colors last:border-0"
        >
          <td
            v-for="col in columns"
            :key="col.key"
            class="text-muted-foreground p-2 align-middle whitespace-nowrap"
            :class="col.class"
          >
            <slot :name="`cell-${col.key}`" :row="row" :value="row[col.key]">
              {{ format(row[col.key]) }}
            </slot>
          </td>
        </tr>
        <tr v-if="data.length === 0">
          <td :colspan="columns.length" class="text-muted-foreground h-16 text-center">—</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts" generic="T extends Record<string, unknown>">
/**
 * Table -- minimal table (shadcn/ui "new-york"): header text-muted-foreground,
 * hairline row borders, empty value --.
 *
 * Bare table only — the solid card shell comes from the calling page (the dot
 * background would show through a translucent shell).
 */
export interface TableColumn {
  key: string;
  title: string;
  class?: string;
}

defineProps<{
  columns: TableColumn[];
  data: T[];
}>();

function format(value: unknown): string {
  if (value === null || value === undefined || value === "") return "—";
  return String(value);
}
</script>
