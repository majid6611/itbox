<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { usePortalStore } from '../../stores/portal'
import { usePortalModulesStore } from '../../stores/portalModules'
import { useChatStore, type TargetKind } from '../../stores/chat'
import type { ChatMessage } from '../../api/types'

const portal = usePortalStore()
const portalModules = usePortalModulesStore()
const chat = useChatStore()

const active = ref<{ kind: TargetKind; name: string | number } | null>(null)
const draft = ref('')
const sending = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)
const threadEl = ref<HTMLElement | null>(null)
const moduleUnavailable = ref(false)
const sendError = ref('')

// The API's error body is JSON ({"detail": "..."}); show just the sentence.
function errorText(e: unknown): string {
  const raw = e instanceof Error ? e.message : ''
  try {
    return JSON.parse(raw).detail || 'Could not send'
  } catch {
    return raw || 'Could not send'
  }
}

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

// A call is just a room link — video-jitsi needs no accounts and no
// directory integration, so "starting a call" is nothing more than
// picking an unguessable room id and sharing the URL, the same way
// sharing any other link into the thread already works. Whoever starts it
// also gets it opened for them immediately, so they're not stuck clicking
// their own just-sent message to join.
function randomRoomId() {
  const bytes = new Uint8Array(8)
  crypto.getRandomValues(bytes)
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
}
async function startCall() {
  if (!active.value || !portalModules.videoCallBaseURL) return
  // The link posted to chat is the bare room URL, shared by everyone —
  // the display-name prefill below is added per-click (see
  // joinURL/callLinkHref) using whoever's actually opening it, not baked
  // in here as the sender's own name.
  const url = `${portalModules.videoCallBaseURL}/${randomRoomId()}`
  await chat.sendMessage(active.value.kind, active.value.name, url)
  window.open(joinURL(url), '_blank', 'noopener')
}

// A message that's nothing but a URL renders as a real link instead of
// inert text — covers a shared video-call room link and any other pasted
// link the same way. The href only ever comes from our own stored/scheme-
// checked message content, never bound from anything unvalidated.
function isPlainUrl(content: string): boolean {
  return /^https?:\/\/\S+$/.test(content.trim())
}
function isVideoCallLink(content: string): boolean {
  return !!portalModules.videoCallBaseURL && content.trim().startsWith(portalModules.videoCallBaseURL + '/')
}
// Pre-fills the join screen with the current viewer's own name — Jitsi
// reads this straight off the URL fragment, no server-side awareness of
// who's joining needed. Every click computes its own, so the same shared
// link fills in "milad" for milad and "mehdi" for mehdi rather than
// whoever happened to start the call.
function joinURL(url: string): string {
  return `${url}#userInfo.displayName=${encodeURIComponent(JSON.stringify(portal.username))}`
}
function callLinkHref(content: string): string {
  return isVideoCallLink(content) ? joinURL(content.trim()) : content.trim()
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
function unreadCount(kind: TargetKind, name: string | number) {
  return chat.unread[keyFor(kind, name)] ?? 0
}
function unreadLabel(kind: TargetKind, name: string | number) {
  const n = unreadCount(kind, name)
  return n > 99 ? '99+' : String(n)
}

async function selectTarget(kind: TargetKind, name: string | number) {
  active.value = { kind, name }
  replyingTo.value = null
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
// Keyed on the newest message only, so loading older history (which only
// prepends) and edits don't yank the view to the bottom.
watch(
  () => messages.value[messages.value.length - 1]?.id,
  async () => {
    await nextTick()
    scrollToBottom()
  },
)

const hasOlder = computed(() => !!active.value && !!chat.hasMore[keyFor(active.value.kind, active.value.name)])
const loadingOlder = ref(false)
async function loadOlder() {
  if (!active.value || !threadEl.value || loadingOlder.value) return
  loadingOlder.value = true
  const el = threadEl.value
  const before = el.scrollHeight - el.scrollTop
  try {
    await chat.fetchOlder(active.value.kind, active.value.name)
    await nextTick()
    // Keep the message the reader was looking at where it was.
    el.scrollTop = el.scrollHeight - before
  } finally {
    loadingOlder.value = false
  }
}

// A background tab counts new messages as unread; coming back to the tab
// is what reads them.
function onVisible() {
  if (document.visibilityState === 'visible' && chat.activeKey) chat.markRead(chat.activeKey)
}

async function send() {
  const text = draft.value.trim()
  if (!text || !active.value) return
  draft.value = ''
  showEmoji.value = false
  sending.value = true
  sendError.value = ''
  try {
    await chat.sendMessage(active.value.kind, active.value.name, text, replyingTo.value?.id)
    replyingTo.value = null
  } catch (e) {
    // Put the text back — a refused send (too long, sending too fast) must
    // not eat what the person typed.
    draft.value = text
    sendError.value = errorText(e)
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
  sendError.value = ''
  try {
    await chat.sendFile(active.value.kind, active.value.name, file, '')
  } catch (e) {
    sendError.value = errorText(e)
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

// --- replying ---------------------------------------------------------
const replyingTo = ref<ChatMessage | null>(null)
function startReply(m: ChatMessage) {
  replyingTo.value = m
  draftInput.value?.focus()
}
function snippet(text: string) {
  return text.length > 80 ? text.slice(0, 80) + '…' : text
}

// --- jumping to a message (quote click, search result) ------------------
const highlightId = ref<number | null>(null)
async function scrollToMessage(id: number) {
  await nextTick()
  // After the thread-switch auto-scroll to the bottom has had its turn.
  setTimeout(() => {
    document.getElementById(`msg-${id}`)?.scrollIntoView({ block: 'center' })
    highlightId.value = id
    setTimeout(() => {
      if (highlightId.value === id) highlightId.value = null
    }, 2000)
  }, 60)
}
async function jumpTo(id: number) {
  if (!active.value) return
  await chat.loadUntil(active.value.kind, active.value.name, id)
  await scrollToMessage(id)
}

// --- typing -----------------------------------------------------------
// A once-a-second clock so a "…is typing" notice disappears on its own
// when it lapses.
const now = ref(Date.now())
const clock = setInterval(() => (now.value = Date.now()), 1000)
const typers = computed(() => (active.value ? chat.typingIn(keyFor(active.value.kind, active.value.name), now.value) : []))
const typingText = computed(() => {
  const t = typers.value
  if (t.length === 0) return ''
  if (t.length === 1) return `${t[0]} is typing…`
  if (t.length === 2) return `${t[0]} and ${t[1]} are typing…`
  return 'Several people are typing…'
})
function onDraftInput() {
  if (active.value && draft.value) chat.sendTyping(active.value.kind, active.value.name)
}

// --- links inside a message -----------------------------------------------
// Only http(s) addresses become links, built as text/href pairs and
// rendered by the template — never v-html — so message text can't inject
// markup. Trailing punctuation belongs to the sentence, not the address.
function segments(content: string): { text: string; href?: string }[] {
  const out: { text: string; href?: string }[] = []
  let last = 0
  for (const m of content.matchAll(/https?:\/\/[^\s<>"']+/g)) {
    const url = m[0].replace(/[.,;:!?)\]]+$/, '')
    if (m.index! > last) out.push({ text: content.slice(last, m.index) })
    out.push({ text: url, href: url })
    last = m.index! + url.length
  }
  if (last < content.length) out.push({ text: content.slice(last) })
  return out
}

// --- leaving / removing from a private group ------------------------------
async function leaveGroup() {
  if (active.value?.kind !== 'custom') return
  if (!confirm('Leave this group? You will no longer see its messages.')) return
  await chat.leaveGroup(active.value.name as number, portal.username!)
}
async function removeFromGroup(username: string) {
  if (active.value?.kind !== 'custom') return
  if (!confirm(`Remove ${username} from this group?`)) return
  await chat.leaveGroup(active.value.name as number, username)
}
// The open group disappearing from my list (I left, or was removed) closes it.
watch(
  () => chat.customGroups,
  (groups) => {
    if (active.value?.kind === 'custom' && !groups.some((g) => g.id === active.value!.name)) {
      active.value = null
      chat.clearActive()
    }
  },
)

// --- search -----------------------------------------------------------
const searchQuery = ref('')
const searchResults = ref<ChatMessage[]>([])
const searchDone = ref(false)
let searchTimer: ReturnType<typeof setTimeout> | undefined
watch(searchQuery, (q) => {
  clearTimeout(searchTimer)
  searchDone.value = false
  if (q.trim().length < 2) {
    searchResults.value = []
    return
  }
  searchTimer = setTimeout(async () => {
    try {
      const res = await chat.search(q.trim())
      if (q === searchQuery.value) {
        searchResults.value = res
        searchDone.value = true
      }
    } catch {
      searchResults.value = []
    }
  }, 300)
})
function resultThread(m: ChatMessage): { kind: TargetKind; name: string | number; label: string } {
  if (m.group_name) return { kind: 'group', name: m.group_name, label: `# ${m.group_name}` }
  if (m.custom_group_id) {
    const g = chat.customGroups.find((x) => x.id === m.custom_group_id)
    return { kind: 'custom', name: m.custom_group_id, label: `🔒 ${g?.name ?? 'group'}` }
  }
  const other = m.sender_username === portal.username ? m.recipient_username! : m.sender_username
  return { kind: 'dm', name: other, label: other }
}
async function openResult(m: ChatMessage) {
  const t = resultThread(m)
  await selectTarget(t.kind, t.name)
  await chat.loadUntil(t.kind, t.name, m.id)
  await scrollToMessage(m.id)
}

function formatTime(iso: string) {
  return new Date(iso).toLocaleString()
}

onUnmounted(() => {
  clearInterval(clock)
  clearTimeout(searchTimer)
  document.removeEventListener('visibilitychange', onVisible)
  chat.clearActive()
})

onMounted(async () => {
  document.addEventListener('visibilitychange', onVisible)
  try {
    await Promise.all([chat.fetchChannels(), chat.fetchUsers(), chat.fetchCustomGroups(), chat.fetchUnread()])
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
      <input v-model="searchQuery" class="search-box" type="search" placeholder="Search messages…" />
      <div v-if="searchQuery.trim().length >= 2" class="search-results">
        <p v-if="searchDone && searchResults.length === 0" class="hint">Nothing found.</p>
        <button v-for="m in searchResults" :key="m.id" type="button" class="search-hit" @click="openResult(m)">
          <span class="hint-inline">{{ resultThread(m).label }} · {{ m.sender_username }} · {{ formatTime(m.created_at) }}</span>
          <span>{{ snippet(m.content) }}</span>
        </button>
      </div>
      <template v-else>
      <h2>Channels</h2>
      <ul class="target-list">
        <li v-for="c in chat.channels" :key="c">
          <button :class="{ active: active?.kind === 'group' && active.name === c }" @click="selectTarget('group', c)">
            <span v-if="unreadCount('group', c)" class="unread-count">{{ unreadLabel('group', c) }}</span>
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
            <span v-if="unreadCount('custom', g.id)" class="unread-count">{{ unreadLabel('custom', g.id) }}</span>
            🔒 {{ g.name }}
          </button>
        </li>
      </ul>

      <h2>Direct Messages</h2>
      <ul class="target-list">
        <li v-for="u in chat.users" :key="u.username">
          <button :class="{ active: active?.kind === 'dm' && active.name === u.username }" @click="selectTarget('dm', u.username)">
            <span v-if="unreadCount('dm', u.username)" class="unread-count">{{ unreadLabel('dm', u.username) }}</span>
            <span class="dot" :class="{ online: u.online }"></span>
            {{ u.username }}
          </button>
        </li>
      </ul>
      <p v-if="chat.channels.length === 0 && chat.users.length === 0" class="hint">Nothing to show yet.</p>
      </template>
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
              <span v-for="u in activeGroup?.members" :key="u" class="member-chip">
                {{ u }}
                <button
                  v-if="u !== portal.username && activeGroup?.created_by === portal.username"
                  type="button"
                  class="chip-x"
                  :aria-label="`Remove ${u}`"
                  @click="removeFromGroup(u)"
                >×</button>
              </span>
              <button @click="showAddMember = !showAddMember">+ Add person</button>
              <button type="button" class="secondary" @click="leaveGroup">Leave group</button>
            </div>
            <button v-if="portalModules.videoCallBaseURL" class="secondary" @click="startCall">📹 Start call</button>
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
          <button v-if="hasOlder" type="button" class="load-older" :disabled="loadingOlder" @click="loadOlder">
            {{ loadingOlder ? 'Loading…' : 'Load older messages' }}
          </button>
          <div
            v-for="m in messages"
            :id="`msg-${m.id}`"
            :key="m.id"
            class="message"
            :class="{ mine: m.sender_username === portal.username, highlight: highlightId === m.id }"
          >
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
                <input v-model="editDraft" type="text" maxlength="4000" autofocus />
                <button type="submit" :disabled="!editDraft.trim()">Save</button>
                <button type="button" @click="cancelEdit">Cancel</button>
              </form>
            </template>
            <template v-else>
              <button v-if="m.reply_to" type="button" class="quote" @click="jumpTo(m.reply_to.id)">
                <strong>{{ m.reply_to.sender_username }}</strong>
                <span v-if="m.reply_to.deleted" class="deleted-tombstone">message deleted</span>
                <span v-else>{{ snippet(m.reply_to.content) }}</span>
              </button>
              <a
                v-if="m.content && isPlainUrl(m.content)"
                :href="callLinkHref(m.content)"
                target="_blank"
                rel="noopener noreferrer"
                class="attachment-link"
                :class="{ 'call-link': isVideoCallLink(m.content) }"
              >{{ isVideoCallLink(m.content) ? '📹 Join video call' : m.content.trim() }}</a>
              <p v-else-if="m.content">
                <template v-for="(seg, i) in segments(m.content)" :key="i">
                  <a v-if="seg.href" :href="seg.href" target="_blank" rel="noopener noreferrer">{{ seg.text }}</a>
                  <template v-else>{{ seg.text }}</template>
                </template>
              </p>
              <a v-if="m.attachment" :href="`/api/portal/chat/attachments/${m.attachment.id}`" target="_blank" rel="noopener" class="attachment-link">
                📎 {{ m.attachment.filename }} ({{ Math.ceil(m.attachment.size_bytes / 1024) }} KB)
              </a>
              <div class="message-actions">
                <button type="button" @click="startReply(m)">Reply</button>
                <template v-if="m.sender_username === portal.username">
                  <button type="button" @click="startEdit(m)">Edit</button>
                  <button type="button" @click="removeMessage(m.id)">Delete</button>
                </template>
              </div>
            </template>
          </div>
          <p v-if="messages.length === 0" class="hint">No messages yet — say hello.</p>
        </div>

        <div v-if="showEmoji" class="emoji-picker">
          <button v-for="e in EMOJI" :key="e" type="button" class="emoji-btn" @click="insertEmoji(e)">{{ e }}</button>
        </div>
        <p class="typing-line">{{ typingText }}</p>
        <div v-if="replyingTo" class="reply-bar">
          <span>Replying to <strong>{{ replyingTo.sender_username }}</strong>: {{ snippet(replyingTo.content) }}</span>
          <button type="button" aria-label="Cancel reply" @click="replyingTo = null">×</button>
        </div>
        <p v-if="sendError" class="send-error">{{ sendError }}</p>
        <form class="composer" @submit.prevent="send">
          <button type="button" :disabled="sending" @click="showEmoji = !showEmoji">😊</button>
          <button type="button" :disabled="sending" @click="fileInput?.click()">📎</button>
          <input ref="fileInput" type="file" hidden @change="pickFile" />
          <input ref="draftInput" v-model="draft" type="text" maxlength="4000" placeholder="Type a message…" :disabled="sending" @input="onDraftInput" />
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
.unread-count {
  min-width: 1.1rem;
  padding: 0 0.3rem;
  border-radius: 999px;
  background: var(--accent);
  color: #fff;
  font-size: 0.7rem;
  line-height: 1.1rem;
  text-align: center;
  flex-shrink: 0;
}
.search-box {
  width: 100%;
  margin-bottom: 0.6rem;
}
.search-results {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
}
.search-hit {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 0.1rem;
  text-align: left;
  font-size: 0.85rem;
}
.member-chip {
  display: inline-flex;
  align-items: center;
  gap: 0.2rem;
  padding: 0.05rem 0.5rem;
  border-radius: 999px;
  background: var(--accent-soft);
  font-size: 0.8rem;
}
.chip-x {
  padding: 0 0.2rem;
  border: 0;
  background: none;
  cursor: pointer;
  color: inherit;
}
.quote {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 0.05rem;
  margin-bottom: 0.25rem;
  padding: 0.2rem 0.6rem;
  border: 0;
  border-left: 3px solid var(--accent);
  border-radius: 4px;
  background: var(--accent-soft);
  font-size: 0.8rem;
  text-align: left;
  cursor: pointer;
}
.message.highlight {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
  border-radius: 8px;
}
.reply-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 0.5rem;
  margin: 0 0 0.25rem;
  padding: 0.25rem 0.6rem;
  border-left: 3px solid var(--accent);
  background: var(--accent-soft);
  font-size: 0.85rem;
}
.typing-line {
  min-height: 1.2rem;
  margin: 0 0.25rem;
  font-size: 0.8rem;
  color: var(--text-muted, inherit);
}
.load-older {
  align-self: center;
  margin-bottom: 0.5rem;
  font-size: 0.85rem;
}
.send-error {
  margin: 0 1rem 0.25rem;
  color: var(--danger-text, #b42318);
  font-size: 0.85rem;
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
.attachment-link.call-link {
  background: var(--accent);
  color: var(--accent-contrast);
}
.message.mine .attachment-link.call-link {
  background: var(--accent-contrast);
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

/* Phones: the people/channels list goes on top as a short scrolling strip,
 * and the conversation gets the full width below it. */
@media (max-width: 700px) {
  .chat-layout {
    flex-direction: column;
    align-items: stretch;
    gap: 0.75rem;
    height: calc(100vh - 7rem);
  }
  .sidebar {
    width: 100%;
    height: auto;
    max-height: 8.5rem;
    border-right: none;
    border-bottom: 1px solid var(--border);
    padding: 0 0 0.5rem;
  }
  .thread-pane {
    flex: 1;
    min-height: 0;
    height: auto;
  }
  .thread-header {
    flex-wrap: wrap;
    gap: 0.35rem 0.75rem;
  }
}
</style>
