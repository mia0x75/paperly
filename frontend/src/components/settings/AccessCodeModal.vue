<template>
  <Modal :show="show" title="访问链接（仅显示一次）" @close="emit('close')">
    <p class="text-muted-foreground mt-0 mb-3 text-sm">
      学生「{{ kidName }}」的访问链接如下。原链接已失效，**仅显示一次**，请立即复制保存。
    </p>
    <div class="flex items-center gap-2">
      <input :value="url" readonly type="text" :class="[FIELD_BASE, 'h-9 flex-1']" />
    </div>
    <p v-if="copied" class="text-success mt-2 text-sm">已复制到剪贴板</p>

    <template #footer>
      <Button variant="outline" class="w-full sm:w-auto" @click="copy">复制</Button>
      <Button variant="outline" class="w-full sm:w-auto" @click="emit('close')">关闭</Button>
    </template>
  </Modal>
</template>

<script setup lang="ts">
import { ref } from "vue";
import Button from "../ui/Button.vue";
import Modal from "../Modal.vue";
import { FIELD_BASE } from "../../lib/ui";

/**
 * AccessCodeModal — shows a one-time access link.
 *
 * access_code is returned only in the create/reset response (no searchable plaintext
 * copy server-side) — once closed it can never be retrieved, hence the "shown once" warning.
 */
const props = defineProps<{ show: boolean; kidName: string; url: string }>();
const emit = defineEmits<{ close: [] }>();

const copied = ref(false);

async function copy(): Promise<void> {
  try {
    await navigator.clipboard.writeText(props.url);
    copied.value = true;
  } catch {
    // Swallow clipboard denial silently (manual select-all still works).
  }
}
</script>
