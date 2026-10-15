<template>
  <div class="flex flex-col gap-3">
    <div v-if="editor" class="flex flex-wrap items-center gap-1">
      <Button
        type="button"
        size="icon"
        class="size-11 sm:size-9"
        variant="ghost"
        :disabled="submitting"
        @click="editor.chain().focus().undo().run()"
      >
        <Icon name="undo" :size="20" />
      </Button>
      <Button
        type="button"
        size="icon"
        class="size-11 sm:size-9"
        variant="ghost"
        :disabled="submitting"
        @click="editor.chain().focus().redo().run()"
      >
        <Icon name="redo" :size="20" />
      </Button>

      <span class="bg-border mx-1 h-6 w-px" />

      <Button
        type="button"
        size="icon"
        class="size-11 sm:size-9"
        variant="ghost"
        :disabled="submitting"
        @click="editor.chain().focus().toggleBold().run()"
      >
        <Icon name="format_bold" :size="20" />
      </Button>
      <Button
        type="button"
        size="icon"
        class="size-11 sm:size-9"
        variant="ghost"
        :disabled="submitting"
        @click="editor.chain().focus().toggleItalic().run()"
      >
        <Icon name="format_italic" :size="20" />
      </Button>
      <Button
        type="button"
        size="icon"
        class="size-11 sm:size-9"
        variant="ghost"
        :disabled="submitting"
        @click="editor.chain().focus().toggleUnderline().run()"
      >
        <Icon name="format_underlined" :size="20" />
      </Button>

      <span class="bg-border mx-1 h-6 w-px" />

      <Button
        type="button"
        size="icon"
        class="size-11 sm:size-9"
        variant="ghost"
        :disabled="submitting"
        @click="editor.chain().focus().toggleHeading({ level: 1 }).run()"
      >
        <Icon name="format_h1" :size="26" />
      </Button>
      <Button
        type="button"
        size="icon"
        class="size-11 sm:size-9"
        variant="ghost"
        :disabled="submitting"
        @click="editor.chain().focus().toggleHeading({ level: 2 }).run()"
      >
        <Icon name="format_h2" :size="26" />
      </Button>
      <Button
        type="button"
        size="icon"
        class="size-11 sm:size-9"
        variant="ghost"
        :disabled="submitting"
        @click="editor.chain().focus().toggleHeading({ level: 3 }).run()"
      >
        <Icon name="format_h3" :size="26" />
      </Button>
      <Button
        type="button"
        size="icon"
        class="size-11 sm:size-9"
        variant="ghost"
        :disabled="submitting"
        @click="editor.chain().focus().toggleHeading({ level: 4 }).run()"
      >
        <Icon name="format_h4" :size="26" />
      </Button>

      <span class="bg-border mx-1 h-6 w-px" />

      <Button
        type="button"
        size="icon"
        class="size-11 sm:size-9"
        variant="ghost"
        :disabled="submitting"
        @click="editor.chain().focus().toggleBulletList().run()"
      >
        <Icon name="format_list_bulleted" :size="20" />
      </Button>
      <Button
        type="button"
        size="icon"
        class="size-11 sm:size-9"
        variant="ghost"
        :disabled="submitting"
        @click="editor.chain().focus().toggleOrderedList().run()"
      >
        <Icon name="format_list_numbered" :size="20" />
      </Button>

      <span class="bg-border mx-1 h-6 w-px" />

      <Button
        type="button"
        size="icon"
        class="size-11 sm:size-9"
        variant="ghost"
        :disabled="submitting"
        @click="editor.chain().focus().unsetAllMarks().clearNodes().run()"
      >
        <Icon name="format_clear" :size="20" />
      </Button>
    </div>

    <div class="bg-background rounded-md border border-border/50 px-3 py-2">
      <EditorContent :editor="editor" />
    </div>

    <div class="flex items-center justify-between gap-3">
      <span class="text-muted-foreground text-xs">追加后不可修改，仅可删除单条。</span>
      <Button icon="add" :loading="submitting" :disabled="!canSubmit" @click="submit">追加总结</Button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref } from "vue";
import { EditorContent, useEditor } from "@tiptap/vue-3";
import StarterKit from "@tiptap/starter-kit";
import Button from "./ui/Button.vue";
import Icon from "./Icon.vue";

/**
 * SummaryEditor -- Tiptap rich-text editor for appending one signed summary segment.
 *
 * StarterKit without horizontalRule: a rule would mimic a segment separator and break
 * the split-by-marker model. Toolbar active flags track via editor events — the Vue
 * binding's shallowRef doesn't re-render on transactions.
 */
const props = defineProps<{ submitting?: boolean }>();
const emit = defineEmits<{ submit: [html: string] }>();

const active = reactive({ heading: false, bold: false, italic: false, bulletList: false, orderedList: false });
const editorVersion = ref(0);
const headingMenuOpen = ref(false);

function refresh(): void {
  const e = editor.value;
  if (!e) return;
  active.heading = e.isActive("heading", { level: 1 }) || e.isActive("heading", { level: 2 }) || e.isActive("heading", { level: 3 }) || e.isActive("heading", { level: 4 });
  active.bold = e.isActive("bold");
  active.italic = e.isActive("italic");
  active.bulletList = e.isActive("bulletList");
  active.orderedList = e.isActive("orderedList");
  editorVersion.value++;
}

function toggleHeadingMenu(): void {
  headingMenuOpen.value = !headingMenuOpen.value;
}

function setHeading(level: 2 | 3 | 4): void {
  if (!editor.value) return;
  editor.value.chain().focus().setHeading({ level }).run();
  headingMenuOpen.value = false;
}

const editor = useEditor({
  extensions: [StarterKit.configure({ horizontalRule: false })],
  content: "",
  editorProps: {
    attributes: { class: "summary-content min-h-24 focus:outline-none" },
  },
  onSelectionUpdate: refresh,
  onTransaction: refresh,
});

const canSubmit = computed(() => {
  void editorVersion.value;
  return !props.submitting && editor.value !== undefined && !editor.value.isEmpty;
});

function submit(): void {
  if (!canSubmit.value || !editor.value) return;
  emit("submit", editor.value.getHTML());
}

/** Reset the editor (parent calls after a successful append). */
function clear(): void {
  editor.value?.commands.clearContent(true);
  refresh();
}

defineExpose({ clear });
</script>
