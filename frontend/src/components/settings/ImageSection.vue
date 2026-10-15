<template>
  <section id="section-image" class="bg-card text-card-foreground flex flex-col gap-4 rounded-xl border p-6 shadow-sm">
    <div class="flex items-center justify-between">
      <h2 class="text-base font-semibold tracking-tight">图片压缩</h2>
      <Button variant="outline" icon="refresh" :loading="loading" @click="load">刷新</Button>
    </div>

    <p class="text-muted-foreground m-0 text-sm font-normal">
      上传的试卷图片会统一转为 WebP：先按 EXIF 方向校正，长边超过 5120px 再等比缩放。
      档位越靠后体积越小，但细小文字越可能失真。
    </p>

    <ErrorState v-if="error !== ''" :message="error" retry-label="重试" @retry="load" />

    <template v-else>
      <div class="flex flex-col gap-2">
        <label
          v-for="opt in WEBP_PRESET_OPTIONS"
          :key="opt.value"
          class="hover:bg-accent/50 flex cursor-pointer items-center gap-3 rounded-lg border p-3 transition-all duration-150 ease-out"
          :class="preset === opt.value && 'border-primary/60 bg-accent/40'"
        >
          <input
            type="radio"
            name="webp-preset"
            class="accent-primary size-4"
            :value="opt.value"
            :checked="preset === opt.value"
            :disabled="saving"
            @change="save(opt.value)"
          />
          <span class="flex flex-col">
            <span class="text-sm font-medium">{{ opt.value }}. {{ opt.label }}</span>
            <span class="text-muted-foreground text-xs font-normal">{{ opt.hint }}</span>
          </span>
        </label>
      </div>
      <p v-if="saved" class="text-success m-0 text-sm">已保存，新上传的图片立即按此档位压缩。</p>
    </template>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import Button from "../ui/Button.vue";
import ErrorState from "../ErrorState.vue";
import { settingsApi, WEBP_PRESET_OPTIONS } from "../../api/settings";

/**
 * ImageSection — the "image compression" section of the settings page.
 *
 * The backend only carries the compress_preset number; tier copy lives on the front end
 * (WEBP_PRESET_OPTIONS) so descriptions aren't written twice and drift.
 */
const preset = ref(3);
const loading = ref(false);
const saving = ref(false);
const saved = ref(false);
const error = ref("");

async function load(): Promise<void> {
  loading.value = true;
  error.value = "";
  saved.value = false;
  try {
    const res = await settingsApi.imageSettings();
    preset.value = res.compress_preset;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    loading.value = false;
  }
}

async function save(value: number): Promise<void> {
  saving.value = true;
  error.value = "";
  saved.value = false;
  try {
    const res = await settingsApi.updateImageSettings(value);
    preset.value = res.compress_preset;
    saved.value = true;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    saving.value = false;
  }
}

onMounted(load);
</script>
