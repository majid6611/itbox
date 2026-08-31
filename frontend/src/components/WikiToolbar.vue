<script setup lang="ts">
import { ref } from 'vue'
import type { Editor } from '@tiptap/vue-3'

const props = defineProps<{
  editor: Editor | undefined
  uploadImage: (file: File) => Promise<string>
}>()

const imageInput = ref<HTMLInputElement | null>(null)
const uploading = ref(false)

function setLink() {
  const editor = props.editor
  if (!editor) return
  const existing = editor.getAttributes('link').href as string | undefined
  const url = window.prompt('Link URL', existing ?? 'https://')
  if (url === null) return
  if (url === '') {
    editor.chain().focus().unsetLink().run()
  } else {
    editor.chain().focus().extendMarkRange('link').setLink({ href: url }).run()
  }
}

async function onImagePicked(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file || !props.editor) return
  uploading.value = true
  try {
    const src = await props.uploadImage(file)
    props.editor.chain().focus().setImage({ src }).run()
  } finally {
    uploading.value = false
  }
}

function insertTable() {
  props.editor?.chain().focus().insertTable({ rows: 3, cols: 3, withHeaderRow: true }).run()
}
</script>

<template>
  <!-- mousedown.prevent stops these buttons from ever taking focus away from
       the ProseMirror editor — without it, the browser moves focus/selection
       to the button on mousedown (before the click handler even runs), and
       editor.chain().focus() only restores focus a beat later, dropping the
       first keystroke or two typed right after a toolbar click. -->
  <div v-if="editor" class="toolbar" @mousedown.prevent>
    <button type="button" :class="{ active: editor.isActive('bold') }" @click="editor.chain().focus().toggleBold().run()">B</button>
    <button type="button" :class="{ active: editor.isActive('italic') }" @click="editor.chain().focus().toggleItalic().run()"><em>I</em></button>
    <button type="button" :class="{ active: editor.isActive('underline') }" @click="editor.chain().focus().toggleUnderline().run()"><u>U</u></button>
    <button type="button" :class="{ active: editor.isActive('strike') }" @click="editor.chain().focus().toggleStrike().run()"><s>S</s></button>
    <span class="sep"></span>
    <button type="button" :class="{ active: editor.isActive('heading', { level: 1 }) }" @click="editor.chain().focus().toggleHeading({ level: 1 }).run()">H1</button>
    <button type="button" :class="{ active: editor.isActive('heading', { level: 2 }) }" @click="editor.chain().focus().toggleHeading({ level: 2 }).run()">H2</button>
    <button type="button" :class="{ active: editor.isActive('heading', { level: 3 }) }" @click="editor.chain().focus().toggleHeading({ level: 3 }).run()">H3</button>
    <span class="sep"></span>
    <button type="button" :class="{ active: editor.isActive('bulletList') }" @click="editor.chain().focus().toggleBulletList().run()">• List</button>
    <button type="button" :class="{ active: editor.isActive('orderedList') }" @click="editor.chain().focus().toggleOrderedList().run()">1. List</button>
    <button type="button" :class="{ active: editor.isActive('codeBlock') }" @click="editor.chain().focus().toggleCodeBlock().run()">Code</button>
    <button type="button" :class="{ active: editor.isActive('blockquote') }" @click="editor.chain().focus().toggleBlockquote().run()">Quote</button>
    <span class="sep"></span>
    <button type="button" :class="{ active: editor.isActive('link') }" @click="setLink">Link</button>
    <button type="button" :disabled="uploading" @click="imageInput?.click()">{{ uploading ? 'Uploading…' : 'Image' }}</button>
    <input ref="imageInput" type="file" accept="image/*" hidden @change="onImagePicked" />
    <button type="button" @click="insertTable">Table</button>
    <span class="sep"></span>
    <button type="button" @click="editor.chain().focus().undo().run()">Undo</button>
    <button type="button" @click="editor.chain().focus().redo().run()">Redo</button>
  </div>
</template>

<style scoped>
.toolbar {
  display: flex;
  gap: 0.2rem;
  flex-wrap: wrap;
  align-items: center;
  border: 1px solid var(--border);
  border-bottom: none;
  border-radius: 10px 10px 0 0;
  padding: 0.4rem;
  background: var(--surface);
}
.toolbar button {
  font-size: 0.8rem;
  font-weight: 600;
  padding: 0.3rem 0.55rem;
  background: transparent;
  color: var(--text-dim);
}
.toolbar button:hover:not(:disabled) {
  background: var(--surface-hover);
  color: var(--text);
}
.toolbar button.active {
  background: var(--accent-soft);
  color: var(--accent);
}
.sep {
  width: 1px;
  align-self: stretch;
  background: var(--border);
  margin: 0 0.2rem;
}
</style>
