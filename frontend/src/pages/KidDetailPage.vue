<template>
  <div class="max-w-3xl space-y-4">
    <div class="flex items-center gap-2">
      <Button variant="ghost" size="sm" icon="arrow_back" @click="router.push('/kids')">返回</Button>
      <h1 class="text-xl font-semibold m-0">{{ kid?.name ?? "学生详情" }}</h1>
      <StatusBadge v-if="kid" :variant="kidStatusVariant(kid.status)" class="ml-auto">
        {{ kidStatusLabel(kid.status) }}
      </StatusBadge>
    </div>

    <ErrorState v-if="error" :message="error" retry-label="重试" @retry="load" />

    <template v-if="kid">
      <!-- Read-only info -->
      <section class="bg-card rounded-xl border p-6 shadow-sm">
        <h2 class="text-muted-foreground mt-0 mb-4 text-sm font-medium">学生信息</h2>
        <dl class="grid grid-cols-1 gap-x-6 gap-y-3 text-sm sm:grid-cols-2">
          <div v-for="row in infoRows" :key="row.label" :class="row.wide ? 'sm:col-span-2' : ''">
            <dt class="text-muted-foreground text-xs">{{ row.label }}</dt>
            <dd class="m-0 break-all">{{ row.value }}</dd>
          </div>
        </dl>

        <div class="mt-4 flex flex-wrap items-center gap-3">
          <Button variant="outline" icon="person" @click="setCurrent">
            {{ isCurrent ? "当前学生" : "设为当前学生" }}
          </Button>
          <Button variant="outline" icon="description" @click="router.push(`/exams?kid_id=${id}`)">
            该生试卷
          </Button>
        </div>
      </section>

      <!-- Edit: the backend PUT is a full-row overwrite, so submit all fields
           (see UpdateKidReq). -->
      <section class="bg-card rounded-xl border p-6 shadow-sm">
        <h2 class="text-muted-foreground mt-0 mb-4 text-sm font-medium">编辑</h2>
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <label class="flex flex-col gap-1 text-sm">
            <span class="text-muted-foreground">姓名 <span class="text-destructive">*</span></span>
            <input v-model="form.name" type="text" :class="[FIELD_BASE, 'h-9']" />
          </label>
          <label class="flex flex-col gap-1 text-sm">
            <span class="text-muted-foreground">年级 <span class="text-destructive">*</span></span>
            <Select v-model="form.grade" :options="GRADE_OPTIONS" placeholder="请选择年级" />
          </label>
          <label class="flex flex-col gap-1 text-sm">
            <span class="text-muted-foreground">性别</span>
            <Select v-model="form.gender" :options="GENDER_OPTIONS" placeholder="未填写" />
          </label>
        </div>
        <div class="mt-4 flex items-center gap-3">
          <Button icon="save" :loading="saving" @click="save">保存</Button>
          <span v-if="saved" class="text-success text-sm">已保存</span>
        </div>
      </section>

      <!-- Account & access: parent resets access code / PIN (第 11 节) -->
      <section class="bg-card rounded-xl border p-6 shadow-sm">
        <h2 class="text-muted-foreground mt-0 mb-4 text-sm font-medium">账号与访问</h2>
        <div class="flex flex-wrap items-center gap-3">
          <Button variant="outline" icon="refresh" @click="codeOpen = true">重置访问码</Button>
          <Button variant="outline" icon="save" @click="pinOpen = true">重置 PIN</Button>
          <span v-if="pinNotice" class="text-success text-sm">{{ pinNotice }}</span>
        </div>

        <!-- The new access code appears only once after reset, and is not auto-hidden. -->
        <div v-if="newAccessUrl" class="mt-4 rounded-md border p-3">
          <p class="mt-0 mb-2 text-sm">旧链接已失效。新访问链接仅显示一次，请立即保存：</p>
          <div class="flex items-center gap-2">
            <input :value="newAccessUrl" readonly type="text" :class="[FIELD_BASE, 'h-9 flex-1']" />
            <Button variant="outline" icon="save" @click="copyAccessUrl">复制</Button>
            <span v-if="copiedAccess" class="text-success text-sm">已复制</span>
          </div>
        </div>
      </section>

      <!-- Dangerous action: deletion confirmation uniformly goes through
           ConfirmDialog (soft-delete semantics). Only active can be deleted —
           graduated is a terminal state, and the backend also rejects deletion. -->
      <section v-if="kid.status === KID_STATUS.ACTIVE" class="border-destructive/40 rounded-xl border p-6">
        <h2 class="text-destructive mt-0 mb-1 text-sm font-medium">删除学生</h2>
        <p class="text-muted-foreground m-0 text-sm">删除后不再显示，数据保留。</p>
        <div class="mt-4">
          <Button variant="destructive" icon="delete" @click="confirmDelete = true">删除学生</Button>
        </div>
      </section>
    </template>

    <p v-else-if="loading" class="text-muted-foreground text-sm">加载中…</p>

    <!-- Reset access code: confirmation -->
    <Modal :show="codeOpen" title="重置访问码" @close="codeOpen = false">
      <p class="m-0">旧链接将立即失效，确定重置？</p>
      <template #footer>
        <Button variant="ghost" :disabled="resettingCode" @click="codeOpen = false">取消</Button>
        <Button :loading="resettingCode" @click="resetAccessCode">确认重置</Button>
      </template>
    </Modal>

    <!-- Reset PIN: leave blank = clear, fill in = set a new PIN -->
    <Modal :show="pinOpen" title="重置 PIN" @close="pinOpen = false">
      <p class="text-muted-foreground m-0">孩子须通过邀请链接设置 PIN。</p>
      <input
        v-model="newPin"
        type="text"
        :class="[FIELD_BASE, 'mt-3 h-9 w-full']"
        placeholder="新 PIN（可留空）"
      />
      <template #footer>
        <Button variant="ghost" :disabled="savingPin" @click="pinOpen = false">取消</Button>
        <Button :loading="savingPin" @click="resetPin">确认</Button>
      </template>
    </Modal>

    <!-- Delete confirmation (soft-delete semantics, unified copy) -->
    <ConfirmDialog
      :show="confirmDelete"
      :title="`确认删除「${kid?.name ?? ''}」？`"
      message="删除后不再显示，数据保留。"
      :loading="deleting"
      @confirm="remove"
      @cancel="confirmDelete = false"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { storeToRefs } from "pinia";
import { useRoute, useRouter } from "vue-router";
import Button from "../components/ui/Button.vue";
import { FIELD_BASE } from "../lib/ui";
import ConfirmDialog from "../components/ConfirmDialog.vue";
import ErrorState from "../components/ErrorState.vue";
import Modal from "../components/Modal.vue";
import Select from "../components/ui/Select.vue";
import StatusBadge from "../components/StatusBadge.vue";
import { kidApi } from "../api/kid";
import { GRADE_OPTIONS, gradeLabelOr } from "../lib/grade";
import { genderLabel, kidStatusLabel, kidStatusVariant } from "../lib/kid";
import { KID_GENDER, KID_STATUS, type KidDetail } from "../api/types";
import type { SelectOption } from "../lib/ui";
import { useKidStore } from "../stores/kid";

/**
 * KidDetailPage: student detail (`/kids/:id`) — view + edit + set current +
 * reset access code/PIN + delete.
 *
 * PUT is a full-row overwrite (nil clears): always submit all fields. kids only has
 * name/grade/gender/avatar — no student-number/class/birthday/tag anywhere here.
 * access_code/pin_hash aren't in the detail DTO: current values never display; a
 * reset shows the new code once, PIN reset answers with a single message.
 */
const GENDER_OPTIONS: readonly SelectOption<number>[] = [
  { value: -1, label: "未填写" },
  { value: KID_GENDER.MALE, label: "男" },
  { value: KID_GENDER.FEMALE, label: "女" },
];

const route = useRoute();
const router = useRouter();
const kidStore = useKidStore();
const { selectedId } = storeToRefs(kidStore);
const id = Number(route.params.id);

const kid = ref<KidDetail | null>(null);
const loading = ref(false);
const error = ref("");
const saving = ref(false);
const saved = ref(false);
const deleting = ref(false);
const confirmDelete = ref(false);

// Access code reset.
const codeOpen = ref(false);
const resettingCode = ref(false);
const newAccessUrl = ref("");
const copiedAccess = ref(false);
// PIN reset.
const pinOpen = ref(false);
const savingPin = ref(false);
const newPin = ref("");
const pinNotice = ref("");

const form = reactive({
  name: "",
  // 0 = not selected; the backend requires grade and it must be 1~12.
  grade: 0,
  // -1 = not filled in; gender Integer 1 male / 0 female.
  gender: -1,
});

const isCurrent = computed(() => selectedId.value === id);

const infoRows = computed(() => {
  const s = kid.value;
  if (!s) return [];
  return [
    { label: "姓名", value: s.name },
    // grade is INTEGER; display via gradeLabelOr(), no stored Chinese.
    { label: "年级", value: gradeLabelOr(s.grade) },
    { label: "性别", value: genderLabel(s.gender) },
    { label: "创建时间", value: s.created_at || "—", wide: true },
  ];
});

async function load(): Promise<void> {
  if (!Number.isFinite(id) || id <= 0) {
    error.value = "无效的学生 ID";
    return;
  }
  loading.value = true;
  error.value = "";
  try {
    const s = await kidApi.get(id);
    kid.value = s;
    form.name = s.name;
    form.grade = s.grade;
    form.gender = s.gender ?? -1;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    loading.value = false;
  }
}

async function save(): Promise<void> {
  error.value = "";
  saved.value = false;
  const name = form.name.trim();
  if (name === "") {
    error.value = "请填写姓名";
    return;
  }
  if (form.grade < 1 || form.grade > 12) {
    error.value = "请选择年级";
    return;
  }
  saving.value = true;
  try {
    // Full-row overwrite: all three fields, gender undefined = not filled in.
    await kidApi.update(id, {
      name,
      grade: form.grade,
      gender: form.gender < 0 ? undefined : form.gender,
    });
    await load();
    await kidStore.fetchList();
    saved.value = true;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    saving.value = false;
  }
}

function setCurrent(): void {
  kidStore.select(id);
}

async function remove(): Promise<void> {
  deleting.value = true;
  error.value = "";
  try {
    await kidApi.remove(id);
    await kidStore.fetchList();
    await router.push("/kids");
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
    deleting.value = false;
  }
}

async function resetAccessCode(): Promise<void> {
  resettingCode.value = true;
  error.value = "";
  try {
    const resp = await kidApi.resetAccessCode(id);
    newAccessUrl.value = `${location.origin}${resp.kid_url}`;
    copiedAccess.value = false;
    codeOpen.value = false;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    resettingCode.value = false;
  }
}

async function copyAccessUrl(): Promise<void> {
  copiedAccess.value = false;
  try {
    await navigator.clipboard.writeText(newAccessUrl.value);
    copiedAccess.value = true;
  } catch {
    error.value = "复制失败，请手动复制";
  }
}

async function resetPin(): Promise<void> {
  savingPin.value = true;
  error.value = "";
  pinNotice.value = "";
  const pin = newPin.value.trim();
  try {
    const resp = await kidApi.resetPin(id, { new_pin: pin });
    pinNotice.value = resp.pin_set ? "PIN 已更新" : "PIN 已清除";
    newPin.value = "";
    pinOpen.value = false;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    savingPin.value = false;
  }
}

onMounted(load);
</script>
