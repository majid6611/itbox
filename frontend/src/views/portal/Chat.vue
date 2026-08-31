<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { usePortalStore } from '../../stores/portal'
import { useChatStore, type TargetKind } from '../../stores/chat'

const portal = usePortalStore()
const chat = useChatStore()

const active = ref<{ kind: TargetKind; name: string | number } | null>(null)
const draft = ref('')
const sending = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)
const threadEl = ref<HTMLElement | null>(null)
const moduleUnavailable = ref(false)

// Emoji themselves need no special support at all — messages are plain
// UTF-8 text (verified end to end: type one, it round-trips through
// Postgres and back exactly as sent) and every modern browser renders
// them natively. This is purely a discoverability convenience — typing
// one via the OS picker (Win+. / Cmd+Ctrl+Space) already worked before
// this button existed.
const EMOJI = ['😀', '😂', '😊', '😍', '🥰', '😎', '🤔', '😢', '😭', '😡', '👍', '👎', '👏', '🙏', '💪', '🎉', '🔥', '❤️', '💯', '✅', '❌', '🚀', '👀', '😴', '🤝', '😅', '🙌', '😮', '🤷', '🎯']
const showEmoji = ref(false)
const draftInput = ref<HTMLInputElement | null>(null)

function insertEmoji(e: string) {
  const el = draftInput.value
  const start = el?.selectionStart ?? draft.value.length
  const end = el?.selectionEnd ?? draft.value.length
  draft.value = draft.value.slice(0, start) + e + draft.value.slice(end)
  nextTick(() => {
    el?.focus()
    const pos = start + e.length
    el?.setSelectionRange(pos, pos)
  })
}

const editingId = ref<number | null>(null)
const editDraft = ref('')

function startEdit(m: { id: number; content: string }) {
  editingId.value = m.id
  editDraft.value = m.content
}
function cancelEdit() {
  editingId.value = null
  editDraft.value = ''
}
async function saveEdit() {
  const content = editDraft.value.trim()
  if (!content || editingId.value === null) return
  await chat.editMessage(editingId.value, content)
  cancelEdit()
}
async function removeMessage(id: number) {
  if (!confirm('Delete this message? This can\'t be undone.')) return
  await chat.deleteMessage(id)
}

const notifPermission = ref<NotificationPermission>('Notification' in window ? Notification.permission : 'denied')
async function enableNotifications() {
  await chat.requestNotificationPermission()
  notifPermission.value = Notification.permission
}

const showNewGroup = ref(false)
const newGroupName = ref('')
const newGroupMembers = ref<Set<string>>(new Set())
const showAddMember = ref(false)
const addMemberName = ref('')

const messages = computed(() => (active.value ? chat.messagesFor(active.value.kind, active.value.name) : []))
const activeGroup = computed(() =>
  active.value?.kind === 'custom' ? chat.customGroups.find((g) => g.id === active.value!.name) : undefined,
)
// Only real employees not already in the group — a free-text field let
// anyone type a nonexistent username in ("dddsds") with no feedback that
// it wasn't a real person.
const addableUsers = computed(() => {
  const current = new Set(activeGroup.value?.members ?? [])
  return chat.users.filter((u) => !current.has(u.username))
})

function keyFor(kind: TargetKind, name: string | number) {
  return `${kind}:${name}`
}
function isUnread(kind: TargetKind, name: string | number) {
  return !!chat.unread[keyFor(kind, name)]
}

async function selectTarget(kind: TargetKind, name: string | number) {
  active.value = { kind, name }
  chat.setActive(kind, name)
  showAddMember.value = false
  if (chat.messagesFor(kind, name).length === 0) {
    await chat.fetchHistory(kind, name)
  }
  await nextTick()
  scrollToBottom()
}

function scrollToBottom() {
  if (threadEl.value) threadEl.value.scrollTop = threadEl.value.scrollHeight
}

// New messages (live or from switching threads) should always pull the
// view down — a chat that doesn't auto-scroll reads as broken.
watch(messages, async () => {
  await nextTick()
  scrollToBottom()
})

async function send() {
  const text = draft.value.trim()
  if (!text || !active.value) return
  draft.value = ''
  showEmoji.value = false
  sending.value = true
  try {
    await chat.sendMessage(active.value.kind, active.value.name, text)
  } finally {
    sending.value = false
  }
}

async function pickFile(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file || !active.value) return
  sending.value = true
  try {
    await chat.sendFile(active.value.kind, active.value.name, file, '')
  } finally {
    sending.value = false
  }
}

function toggleNewGroupMember(username: string) {
  if (newGroupMembers.value.has(username)) newGroupMembers.value.delete(username)
  else newGroupMembers.value.add(username)
}

async function createGroup() {
  const name = newGroupName.value.trim()
  if (!name) return
  await chat.createGroup(name, Array.from(newGroupMembers.value))
  showNewGroup.value = false
  newGroupName.value = ''
  newGroupMembers.value = new Set()
}

async function addMember() {
  const username = addMemberName.value.trim()
  if (!username || active.value?.kind !== 'custom') return
  await chat.addGroupMember(active.value.name as number, username)
  addMemberName.value = ''
  showAddMember.value = false
}

function formatTime(iso: string) {
  return new Date(iso).toLocaleString()
}

onMounted(async () => {
  try {
    await Promise.all([chat.fetchChannels(), chat.fetchUsers(), chat.fetchCustomGroups()])
  } catch {
    moduleUnavailable.value = true
    return
  }
  // The WebSocket itself is connected app-wide from App.vue (so the nav
  // badge and notifications work from any portal page) — this page just
  // needs it to already be up before picking a default thread.
  if (!chat.wsConnected) chat.connectWS(portal.username!)
  if (chat.channels.length > 0) {
    await selectTarget('group', chat.channels[0])
  }
})
</script>

<template>
  <div v-if="moduleUnavailable" class="empty-state">
    <p>Chat isn't available right now — ask your admin to check whether the Chat module is installed.</p>
  </div>
  <div v-else class="chat-layout">
    <aside class="sidebar">
      <h2>Channels</h2>
      <ul class="target-list">
        <li v-for="c in chat.channels" :key="c">
          <button :class="{ active: active?.kind === 'group' && active.name === c }" @click="selectTarget('group', c)">
            <span v-if="isUnread('group', c)" class="dot unread"></span>
            # {{ c }}
          </button>
        </li>
      </ul>

      <h2>
        My Groups
        <button class="new-group-btn" @click="showNewGroup = !showNewGroup">+</button>
      </h2>
      <form v-if="showNewGroup" class="new-group-form" @submit.prevent="createGroup">
        <input v-model="newGroupName" placeholder="Group name" required />
        <p class="hint">Add people:</p>
        <label v-for="u in chat.users" :key="u.username" class="member-check">
          <input type="checkbox" :checked="newGroupMembers.has(u.username)" @change="toggleNewGroupMember(u.username)" />
          {{ u.username }}
        </label>
        <button type="submit">Create</button>
      </form>
      <ul class="target-list">
        <li v-for="g in chat.customGroups" :key="g.id">
          <button :class="{ active: active?.kind === 'custom' && active.name === g.id }" @click="selectTarget('custom', g.id)">
            <span v-if="isUnread('custom', g.id)" class="dot unread"></span>
            🔒 {{ g.name }}
          </button>
        </li>
      </ul>

      <h2>Direct Messages</h2>
      <ul class="target-list">
        <li v-for="u in chat.users" :key="u.username">
          <button :class="{ active: active?.kind === 'dm' && active.name === u.username }" @click="selectTarget('dm', u.username)">
            <span v-if="isUnread('dm', u.username)" class="dot unread"></span>
            <span class="dot" :class="{ online: u.online }"></span>
            {{ u.username }}
          </button>
        </li>
      </ul>
      <p v-if="chat.channels.length === 0 && chat.users.length === 0" class="hint">Nothing to show yet.</p>
    </aside>

    <main class="thread-pane">
      <div v-if="!active" class="empty-state">
        <p>Pick a channel, group, or person to start chatting.</p>
      </div>
      <template v-else>
        <div class="thread-header">
          <h1 v-if="active.kind === 'group'"># {{ active.name }}</h1>
          <h1 v-else-if="active.kind === 'custom'">🔒 {{ activeGroup?.name }}</h1>
          <h1 v-else>
            <span class="dot" :class="{ online: chat.isOnline(String(active.name)) }"></span>
            {{ active.name }}
          </h1>
          <div class="header-actions">
            <div v-if="active.kind === 'custom'" class="group-actions">
              <span class="hint-inline">{{ activeGroup?.members.join(', ') }}</span>
              <button @click="showAddMember = !showAddMember">+ Add person</button>
            </div>
            <button v-if="notifPermission === 'default'" class="notif-btn" @click="enableNotifications">🔔 Enable notifications</button>
            <span v-else-if="notifPermission === 'denied'" class="hint-inline">Notifications blocked in browser settings</span>
          </div>
        </div>
        <form v-if="showAddMember" class="add-member-form" @submit.prevent="addMember">
          <select v-model="addMemberName" required>
            <option value="" disabled>Pick a person…</option>
            <option v-for="u in addableUsers" :key="u.username" :value="u.username">{{ u.username }}</option>
          </select>
          <button type="submit" :disabled="!addMemberName">Add</button>
        </form>
        <p v-else-if="active.kind === 'custom' && addableUsers.length === 0" class="hint">Everyone's already in this group.</p>

        <div ref="threadEl" class="thread">
          <div v-for="m in messages" :key="m.id" class="message" :class="{ mine: m.sender_username === portal.username }">
            <div class="message-meta">
              <strong>{{ m.sender_username }}</strong>
              <span class="hint-inline">{{ formatTime(m.created_at) }}</span>
              <span v-if="m.edited_at && !m.deleted_at" class="hint-inline">(edited)</span>
            </div>
            <template v-if="m.deleted_at">
              <p class="deleted-tombstone">This message was deleted</p>
            </template>
            <template v-else-if="editingId === m.id">
              <form class="edit-form" @submit.prevent="saveEdit">
                <input v-model="editDraft" type="text" autofocus />
                <button type="submit" :disabled="!editDraft.trim()">Save</button>
                <button type="button" @click="cancelEdit">Cancel</button>
              </form>
            </template>
            <template v-else>
              <p v-if="m.content">{{ m.content }}</p>
              <a v-if="m.attachment" :href="`/api/portal/chat/attachments/${m.attachment.id}`" target="_blank" rel="noopener" class="attachment-link">
                📎 {{ m.attachment.filename }} ({{ Math.ceil(m.attachment.size_bytes / 1024) }} KB)
              </a>
              <div v-if="m.sender_username === portal.username" class="message-actions">
                <button type="button" @click="startEdit(m)">Edit</button>
                <button type="button" @click="removeMessage(m.id)">Delete</button>
              </div>
            </template>
          </div>
          <p v-if="messages.length === 0" class="hint">No messages yet — say hello.</p>
        </div>

        <div v-if="showEmoji" class="emoji-picker">
          <button v-for="e in EMOJI" :key="e" type="button" class="emoji-btn" @click="insertEmoji(e)">{{ e }}</button>
        </div>
        <form class="composer" @submit.prevent="send">
          <button type="button" :disabled="sending" @click="showEmoji = !showEmoji">😊</button>
          <button type="button" :disabled="sending" @click="fileInput?.click()">📎</button>
          <input ref="fileInput" type="file" hidden @change="pickFile" />
          <input ref="draftInput" v-model="draft" type="text" placeholder="Type a message…" :disabled="sending" />
          <button type="submit" :disabled="sending || !draft.trim()">Send</button>
        </form>
      </template>
    </main>
  </div>
</template>

<style scoped>
.chat-layout {
  display: flex;
  gap: 2rem;
  align-items: flex-start;
  height: calc(100vh - 8rem);
}
.sidebar {
  width: 16rem;
  flex-shrink: 0;
  border-right: 1px solid var(--border);
  padding-right: 1.25rem;
  overflow-y: auto;
  height: 100%;
}
.sidebar h2 {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 0.72rem;
  font-family: var(--font-ui);
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--text-faint);
  margin: 1.25rem 0 0.4rem;
}
.sidebar h2:first-child {
  margin-top: 0;
}
.new-group-btn {
  font-size: 0.8rem;
  padding: 0 0.45rem;
  line-height: 1.5;
}
.new-group-form {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
  margin-bottom: 0.6rem;
  font-size: 0.85rem;
}
.member-check {
  display: flex;
  align-items: center;
  gap: 0.35rem;
  font-weight: 400;
  text-transform: none;
}
.target-list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.target-list button {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  width: 100%;
  text-align: left;
  background: none;
  border: none;
  padding: 0.35rem 0.5rem;
  cursor: pointer;
  color: var(--text-dim);
  font-weight: 400;
  border-radius: 6px;
  font-size: 0.88rem;
}
.target-list button:hover {
  background: var(--surface-hover);
  color: var(--text);
}
.target-list button.active {
  background: var(--accent-soft);
  color: var(--accent);
  font-weight: 600;
}
.dot {
  width: 0.5rem;
  height: 0.5rem;
  border-radius: 50%;
  background: var(--border-strong);
  flex-shrink: 0;
}
.dot.online {
  background: var(--success-text);
}
.dot.unread {
  background: var(--accent);
}
.thread-pane {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  height: 100%;
}
.thread-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
}
.thread-header h1 {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 1.15rem;
  margin: 0 0 0.5rem;
}
.header-actions {
  display: flex;
  align-items: center;
  gap: 1rem;
}
.group-actions {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.notif-btn,
.group-actions button {
  font-size: 0.8rem;
  padding: 0.35rem 0.65rem;
}
.deleted-tombstone {
  margin: 0.2rem 0 0;
  font-style: italic;
  color: var(--text-faint);
  background: transparent;
  border: 1px dashed var(--border-strong);
  padding: 0.45rem 0.7rem;
  border-radius: 12px;
  display: inline-block;
}
.edit-form {
  display: flex;
  gap: 0.4rem;
  margin-top: 0.2rem;
}
.message-actions {
  display: flex;
  gap: 0.6rem;
  margin-top: 0.25rem;
  opacity: 0;
  font-size: 0.76rem;
  transition: opacity 0.1s ease;
}
.message:hover .message-actions {
  opacity: 1;
}
.message-actions button {
  background: none;
  border: none;
  color: var(--text-faint);
  cursor: pointer;
  padding: 0;
  font-weight: 600;
}
.message-actions button:hover {
  color: var(--text-dim);
}
.add-member-form {
  display: flex;
  gap: 0.5rem;
  margin-bottom: 0.5rem;
}
.thread {
  flex: 1;
  overflow-y: auto;
  border: 1px solid var(--border);
  border-radius: 12px;
  padding: 1rem;
  display: flex;
  flex-direction: column;
  gap: 0.7rem;
  background: var(--surface);
}
.message {
  max-width: 32rem;
}
.message.mine {
  align-self: flex-end;
  text-align: right;
}
.message-meta {
  display: flex;
  gap: 0.5rem;
  align-items: baseline;
  font-size: 0.8rem;
}
.message-meta strong {
  font-family: var(--font-ui);
}
.message.mine .message-meta {
  justify-content: flex-end;
}
/* The message bubble itself — plain text before this always rendered flat
   against the thread background, which read as a list of statements, not
   a conversation. */
.message p {
  margin: 0.25rem 0 0;
  display: inline-block;
  background: var(--bg);
  padding: 0.5rem 0.8rem;
  border-radius: 14px;
  text-align: left;
  line-height: 1.45;
}
.message.mine p {
  background: var(--accent);
  color: var(--accent-contrast);
}
.attachment-link {
  display: inline-block;
  margin-top: 0.25rem;
  font-size: 0.85rem;
  font-weight: 600;
  background: var(--bg);
  padding: 0.45rem 0.75rem;
  border-radius: 14px;
}
.message.mine .attachment-link {
  background: var(--accent-soft);
  color: var(--accent);
}
.emoji-picker {
  display: flex;
  flex-wrap: wrap;
  gap: 0.15rem;
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 0.5rem;
  margin-top: 0.5rem;
  background: var(--surface);
}
.emoji-btn {
  background: none;
  border: none;
  font-size: 1.2rem;
  padding: 0.25rem;
  cursor: pointer;
  border-radius: 6px;
  line-height: 1;
}
.emoji-btn:hover {
  background: var(--surface-hover);
}
.composer {
  display: flex;
  gap: 0.5rem;
  margin-top: 0.85rem;
  align-items: center;
}
.composer input[type='text'] {
  flex: 1;
}
.composer > button[type='button'] {
  background: transparent;
  color: var(--text-dim);
  font-size: 1.05rem;
  padding: 0.45rem 0.6rem;
}
.composer > button[type='button']:hover:not(:disabled) {
  background: var(--surface-hover);
}
.empty-state {
  color: var(--text-dim);
}
.hint {
  font-size: 0.88rem;
  color: var(--text-dim);
}
.hint-inline {
  font-size: 0.8rem;
  color: var(--text-faint);
}
</style>
