<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { EditorContent, useEditor } from '@tiptap/vue-3'
import StarterKit from '@tiptap/starter-kit'
import Link from '@tiptap/extension-link'
import Placeholder from '@tiptap/extension-placeholder'
import Image from '@tiptap/extension-image'
import Underline from '@tiptap/extension-underline'
import Table from '@tiptap/extension-table'
import TableRow from '@tiptap/extension-table-row'
import TableHeader from '@tiptap/extension-table-header'
import TableCell from '@tiptap/extension-table-cell'
import { useWikiStore } from '../../stores/wiki'
import WikiTreeNode, { type WikiTreeNodeData } from '../../components/WikiTreeNode.vue'
import WikiToolbar from '../../components/WikiToolbar.vue'
import type { WikiPageSummary } from '../../api/types'

const route = useRoute()
const router = useRouter()
const wiki = useWikiStore()

const activePath = computed(() => {
  const raw = route.params.pathMatch
  return Array.isArray(raw) ? raw.join('/') : raw ?? ''
})

const editing = ref(false)
const showHistory = ref(false)
const showNewPage = ref(false)
const newPage = ref({ path: '', title: '' })
const titleDraft = ref('')
const saving = ref(false)
const uploadBusy = ref(false)
const previewRevisionId = ref<number | null>(null)
const previewContent = ref('')
const notFound = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)
const searchQuery = ref('')
const searchResults = ref<WikiPageSummary[]>([])
const searching = ref(false)
const showRename = ref(false)
const renamePath = ref('')

// Shared by both editor instances below — a historical revision can
// contain images/tables too, so the read-only preview editor needs the same
// node types registered or ProseMirror's parser (see next comment) would
// silently drop them when displaying an old version.
const contentExtensions = [
  StarterKit,
  Link.configure({ protocols: ['http', 'https'] }),
  Image,
  Underline,
  Table.configure({ resizable: false }),
  TableRow,
  TableHeader,
  TableCell,
]

// Uploads a dropped/pasted/toolbar-picked image through the exact same
// attachment endpoint the "Files" section uses, then returns the URL to
// embed. Requires the page to already exist (it always does by the time
// you're editing — a new page is created with empty content first).
async function uploadImageFile(file: File): Promise<string> {
  const attachment = await wiki.uploadAttachment(activePath.value, file)
  return `/api/portal/wiki/attachments/${attachment.id}`
}

// Content is always rendered through this Tiptap/ProseMirror instance,
// never via v-html — ProseMirror's HTML parser only keeps tags/marks its
// schema knows about, which strips <script> and other injected markup.
// Wiki content is shared between employees with no server-side sanitizer,
// so this is the thing standing between one employee's page and stored XSS
// against another's session.
const editor = useEditor({
  extensions: [...contentExtensions, Placeholder.configure({ placeholder: 'Write something…' })],
  editable: false,
  content: '',
  editorProps: {
    handleDrop(view, event, _slice, moved) {
      if (moved) return false // internal drag (e.g. reordering selection), not a file drop
      const files = Array.from(event.dataTransfer?.files ?? []).filter((f) => f.type.startsWith('image/'))
      if (!files.length) return false
      event.preventDefault()
      const coords = view.posAtCoords({ left: event.clientX, top: event.clientY })
      const pos = coords?.pos ?? view.state.selection.from
      for (const file of files) {
        uploadImageFile(file).then((src) => {
          const node = view.state.schema.nodes.image.create({ src })
          view.dispatch(view.state.tr.insert(pos, node))
        })
      }
      return true
    },
    handlePaste(_view, event) {
      const item = Array.from(event.clipboardData?.items ?? []).find((i) => i.type.startsWith('image/'))
      const file = item?.getAsFile()
      if (!file) return false
      event.preventDefault()
      uploadImageFile(file).then((src) => {
        editor.value?.chain().focus().setImage({ src }).run()
      })
      return true
    },
  },
})

// Same reasoning as `editor` above — old revisions are also rendered
// through ProseMirror's schema-constrained parser rather than v-html, so a
// malicious historical revision can't execute script when previewed either.
const previewEditor = useEditor({
  extensions: contentExtensions,
  editable: false,
  content: '',
})

onBeforeUnmount(() => {
  editor.value?.destroy()
  previewEditor.value?.destroy()
})

// The tree is built from every path segment, not just full page paths, so
// "engineering/onboarding/setup-guide" shows as nested folders even though
// only the full path is a real, clickable page.
const tree = computed<WikiTreeNodeData[]>(() => {
  const root: WikiTreeNodeData[] = []
  const index = new Map<string, WikiTreeNodeData>()
  for (const p of wiki.pages) {
    const parts = p.path.split('/')
    let path = ''
    let siblings = root
    for (let i = 0; i < parts.length; i++) {
      path = path ? `${path}/${parts[i]}` : parts[i]
      let node = index.get(path)
      if (!node) {
        node = { name: parts[i], fullPath: path, children: [] }
        index.set(path, node)
        siblings.push(node)
      }
      if (i === parts.length - 1) node.page = p as WikiPageSummary
      siblings = node.children
    }
  }
  return root
})

// Set right before navigating to a freshly-created page, so loadPage below
// knows to land in edit mode once its fetch resolves — router.push()
// doesn't wait for this watcher's async fetch to finish, so setting
// `editing` before navigating and letting loadPage unconditionally reset it
// afterward loses the race and silently drops back to read-only.
const pendingEditPath = ref<string | null>(null)

async function loadPage(path: string) {
  showHistory.value = false
  previewRevisionId.value = null
  notFound.value = false
  if (!path) {
    editing.value = false
    wiki.current = null
    editor.value?.commands.setContent('')
    return
  }
  try {
    await wiki.fetchPage(path)
    titleDraft.value = wiki.current?.title ?? ''
    editor.value?.commands.setContent(wiki.current?.content ?? '')
    const startEditing = pendingEditPath.value === path
    pendingEditPath.value = null
    editing.value = startEditing
    editor.value?.setEditable(startEditing)
  } catch {
    editing.value = false
    notFound.value = true
    wiki.current = null
  }
}

watch(activePath, (p) => loadPage(p), { immediate: true })

const moduleUnavailable = ref(false)

async function refreshPages() {
  try {
    await wiki.fetchPages()
  } catch {
    // Most likely the wiki module isn't installed — its path route
    // wouldn't exist, so this request 404s before ever reaching it.
    moduleUnavailable.value = true
  }
}
refreshPages()

function startEdit() {
  editing.value = true
  editor.value?.setEditable(true)
}

function cancelEdit() {
  editing.value = false
  editor.value?.setEditable(false)
  editor.value?.commands.setContent(wiki.current?.content ?? '')
  titleDraft.value = wiki.current?.title ?? ''
}

async function save() {
  if (!editor.value) return
  saving.value = true
  try {
    await wiki.savePage(activePath.value, titleDraft.value, editor.value.getHTML())
    await wiki.fetchPage(activePath.value)
    editing.value = false
    editor.value.setEditable(false)
    if (showHistory.value) await wiki.fetchRevisions(activePath.value)
  } finally {
    saving.value = false
  }
}

function startNewPageAt(path: string) {
  newPage.value.path = path
  showNewPage.value = true
}

async function createPage() {
  const path = newPage.value.path.trim().replace(/^\/+|\/+$/g, '')
  const title = newPage.value.title.trim()
  if (!path || !title) return
  await wiki.savePage(path, title, '')
  showNewPage.value = false
  newPage.value = { path: '', title: '' }
  if (path === activePath.value) {
    // Already on this path (the "Create it" button on a not-found page
    // pre-fills the current path) — pushing the same path wouldn't change
    // the route param, so the watcher below would never re-fire. Load
    // directly instead.
    await loadPage(path)
    editing.value = true
    editor.value?.setEditable(true)
  } else {
    pendingEditPath.value = path
    router.push(`/portal/wiki/${path}`)
  }
}

async function openHistory() {
  showHistory.value = !showHistory.value
  previewRevisionId.value = null
  if (showHistory.value) await wiki.fetchRevisions(activePath.value)
}

async function previewRevision(id: number) {
  previewRevisionId.value = id
  previewContent.value = await wiki.fetchRevisionContent(activePath.value, id)
  previewEditor.value?.commands.setContent(previewContent.value)
}

async function restoreRevision() {
  if (!wiki.current || previewContent.value === undefined) return
  await wiki.savePage(activePath.value, wiki.current.title, previewContent.value)
  await loadPage(activePath.value)
  showHistory.value = false
}

function formatTime(iso: string) {
  return new Date(iso).toLocaleString()
}

let searchDebounce: ReturnType<typeof setTimeout> | undefined
watch(searchQuery, (q) => {
  clearTimeout(searchDebounce)
  if (!q.trim()) {
    searchResults.value = []
    return
  }
  searchDebounce = setTimeout(async () => {
    searching.value = true
    try {
      searchResults.value = await wiki.search(q)
    } finally {
      searching.value = false
    }
  }, 250)
})

function goToSearchResult(path: string) {
  searchQuery.value = ''
  searchResults.value = []
  router.push(`/portal/wiki/${path}`)
}

function startRename() {
  renamePath.value = activePath.value
  showRename.value = true
}

async function confirmRename() {
  const target = renamePath.value.trim().replace(/^\/+|\/+$/g, '')
  if (!target || target === activePath.value) {
    showRename.value = false
    return
  }
  await wiki.renamePage(activePath.value, target)
  showRename.value = false
  router.push(`/portal/wiki/${target}`)
}

async function deletePage() {
  if (!wiki.current) return
  if (!confirm(`Delete "${wiki.current.title}" and its entire history? This can't be undone.`)) return
  await wiki.deletePage(activePath.value)
  router.push({ name: 'portal-wiki', params: { pathMatch: [] } })
}

async function uploadFile(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  uploadBusy.value = true
  try {
    await wiki.uploadAttachment(activePath.value, file)
  } finally {
    uploadBusy.value = false
    input.value = ''
  }
}

watch(
  () => wiki.current?.id,
  async () => {
    if (wiki.current) await wiki.fetchAttachments(activePath.value)
  },
)
</script>

<template>
  <div v-if="moduleUnavailable" class="empty-state">
    <p>The wiki isn't available right now — ask your admin to check whether the Wiki module is installed.</p>
  </div>
  <div v-else class="wiki-layout">
    <aside class="sidebar">
      <input v-model="searchQuery" type="search" class="search-box" placeholder="Search pages…" />
      <ul v-if="searchQuery.trim()" class="search-results">
        <li v-if="searching" class="hint">Searching…</li>
        <template v-else>
          <li v-for="r in searchResults" :key="r.id">
            <button class="link-btn" @click="goToSearchResult(r.path)">{{ r.title }}</button>
            <span class="hint-inline"> — {{ r.path }}</span>
          </li>
          <li v-if="searchResults.length === 0" class="hint">No matches.</li>
        </template>
      </ul>
      <template v-else>
        <button class="new-page-btn" @click="showNewPage = !showNewPage">+ New page</button>
        <form v-if="showNewPage" class="new-page-form" @submit.prevent="createPage">
          <input v-model="newPage.path" placeholder="category/sub/page" required />
          <input v-model="newPage.title" placeholder="Title" required />
          <button type="submit">Create</button>
        </form>
        <ul class="tree">
          <WikiTreeNode v-for="n in tree" :key="n.fullPath" :node="n" :active-path="activePath" />
        </ul>
        <p v-if="!wiki.loading && wiki.pages.length === 0" class="hint">No pages yet.</p>
      </template>
    </aside>

    <main class="content">
      <div v-if="!activePath" class="empty-state">
        <p>Pick a page from the sidebar, or create a new one.</p>
      </div>
      <div v-else-if="notFound" class="empty-state">
        <p>No page at "{{ activePath }}" yet.</p>
        <button @click="startNewPageAt(activePath)">Create it</button>
      </div>
      <div v-else-if="wiki.current">
        <div class="page-header">
          <input v-if="editing" v-model="titleDraft" class="title-input" />
          <h1 v-else>{{ wiki.current.title }}</h1>
          <div class="actions">
            <template v-if="editing">
              <button :disabled="saving" @click="save">{{ saving ? 'Saving…' : 'Save' }}</button>
              <button :disabled="saving" @click="cancelEdit">Cancel</button>
            </template>
            <template v-else>
              <button v-if="wiki.current.can_write" @click="startEdit">Edit</button>
              <button @click="openHistory">{{ showHistory ? 'Hide history' : 'History' }}</button>
              <button v-if="wiki.current.can_write" @click="startRename">Rename</button>
              <button v-if="wiki.current.can_write" class="danger" @click="deletePage">Delete</button>
            </template>
          </div>
        </div>
        <form v-if="showRename" class="rename-form" @submit.prevent="confirmRename">
          <input v-model="renamePath" placeholder="new/category/path" required />
          <button type="submit">Move</button>
          <button type="button" @click="showRename = false">Cancel</button>
        </form>
        <p class="hint-inline">Last updated {{ formatTime(wiki.current.updated_at) }}</p>

        <div v-if="showHistory" class="history-panel">
          <ul>
            <li v-for="r in wiki.revisions" :key="r.id">
              <button class="link-btn" @click="previewRevision(r.id)">
                {{ formatTime(r.created_at) }} — {{ r.author }}
              </button>
            </li>
          </ul>
          <div v-if="previewRevisionId !== null" class="revision-preview">
            <p class="hint-inline">Previewing an older version.</p>
            <button @click="restoreRevision">Restore this version</button>
            <EditorContent :editor="previewEditor" class="tiptap-view" />
          </div>
        </div>

        <template v-else>
          <WikiToolbar v-if="editing" :editor="editor" :upload-image="uploadImageFile" />
          <EditorContent :editor="editor" :class="['tiptap', { editable: editing }]" />
        </template>

        <section class="attachments">
          <h2>Files</h2>
          <ul>
            <li v-for="a in wiki.attachments" :key="a.id">
              <a :href="`/api/portal/wiki/attachments/${a.id}`" target="_blank" rel="noopener">{{ a.filename }}</a>
              <span class="hint-inline"> ({{ Math.ceil(a.size_bytes / 1024) }} KB)</span>
            </li>
          </ul>
          <input ref="fileInput" type="file" :disabled="uploadBusy" @change="uploadFile" />
        </section>
      </div>
    </main>
  </div>
</template>

<style scoped>
.wiki-layout {
  display: flex;
  gap: 1.5rem;
  align-items: flex-start;
}
.sidebar {
  width: 16rem;
  flex-shrink: 0;
  border-right: 1px solid #333;
  padding-right: 1rem;
}
.search-box {
  width: 100%;
  margin-bottom: 0.5rem;
  box-sizing: border-box;
}
.search-results {
  list-style: none;
  margin: 0;
  padding: 0;
}
.search-results li {
  padding: 0.3rem 0;
  font-size: 0.9rem;
}
.new-page-btn {
  width: 100%;
  margin-bottom: 0.5rem;
}
.new-page-form {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
  margin-bottom: 0.75rem;
}
.tree {
  margin: 0;
  padding: 0;
}
.content {
  flex: 1;
  min-width: 0;
}
.empty-state {
  opacity: 0.7;
}
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 1rem;
}
.page-header h1 {
  margin: 0;
  font-size: 1.4rem;
}
.title-input {
  font-size: 1.4rem;
  font-weight: bold;
  flex: 1;
}
.actions {
  display: flex;
  gap: 0.5rem;
  flex-shrink: 0;
}
.actions .danger {
  color: #e5534b;
  border-color: #e5534b;
}
.rename-form {
  display: flex;
  gap: 0.5rem;
  margin-top: 0.5rem;
}
.rename-form input {
  flex: 1;
  max-width: 24rem;
}
.hint {
  font-size: 0.9rem;
  opacity: 0.7;
}
.hint-inline {
  font-size: 0.85rem;
  opacity: 0.7;
}
.history-panel {
  border: 1px solid #333;
  border-radius: 8px;
  padding: 0.75rem;
  margin-top: 0.75rem;
}
.history-panel ul {
  margin: 0;
  padding: 0;
  list-style: none;
}
.link-btn {
  background: none;
  border: none;
  color: #4a9eff;
  cursor: pointer;
  padding: 0.2rem 0;
  font-size: 0.9rem;
}
.revision-preview {
  margin-top: 0.5rem;
  border-top: 1px solid #333;
  padding-top: 0.5rem;
}
.tiptap,
.tiptap-view {
  border: 1px solid transparent;
  border-radius: 6px;
  padding: 0.5rem 0;
  min-height: 4rem;
  line-height: 1.5;
}
.tiptap.editable :deep(.ProseMirror) {
  border: 1px solid #333;
  border-radius: 0 0 6px 6px;
  padding: 0.75rem;
  min-height: 12rem;
  outline: none;
}
/* The Placeholder extension only marks the empty paragraph with a
   data-placeholder attribute and an is-editor-empty class — it ships no
   CSS of its own, so without this rule the editor looks identical whether
   it's empty or genuinely broken. */
.tiptap.editable :deep(p.is-editor-empty:first-child::before) {
  content: attr(data-placeholder);
  float: left;
  height: 0;
  color: #888;
  opacity: 0.8;
  pointer-events: none;
}
/* ProseMirror's table/image nodes render as plain unstyled <table>/<img> —
   without this, an inserted image can show at full native resolution and a
   table has no visible borders, both looking broken rather than embedded. */
.tiptap :deep(img),
.tiptap-view :deep(img) {
  max-width: 100%;
  border-radius: 4px;
}
.tiptap :deep(table),
.tiptap-view :deep(table) {
  border-collapse: collapse;
  margin: 0.5rem 0;
  width: 100%;
}
.tiptap :deep(td),
.tiptap :deep(th),
.tiptap-view :deep(td),
.tiptap-view :deep(th) {
  border: 1px solid #333;
  padding: 0.35rem 0.5rem;
  text-align: left;
}
.tiptap :deep(th),
.tiptap-view :deep(th) {
  background: rgba(255, 255, 255, 0.08);
  font-weight: bold;
}
.attachments {
  margin-top: 2rem;
  border-top: 1px solid #333;
  padding-top: 1rem;
}
.attachments h2 {
  font-size: 1rem;
  margin: 0 0 0.5rem;
}
.attachments ul {
  list-style: none;
  padding: 0;
  margin: 0 0 0.5rem;
}
</style>
