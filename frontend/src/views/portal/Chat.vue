<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { usePortalStore } from '../../stores/portal'
import { useChatStore } from '../../stores/chat'

const portal = usePortalStore()
const chat = useChatStore()

const active = ref<{ kind: 'group' | 'dm'; name: string } | null>(null)
const draft = ref('')
const sending = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)
const threadEl = ref<HTMLElement | null>(null)
const moduleUnavailable = ref(false)

const messages = computed(() => (active.value ? chat.messagesFor(active.value.kind, active.value.name) : []))

async function selectTarget(kind: 'group' | 'dm', name: string) {
  active.value = { kind, name }
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

function formatTime(iso: string) {
  return new Date(iso).toLocaleString()
}

onMounted(async () => {
  try {
    await Promise.all([chat.fetchChannels(), chat.fetchUsers()])
  } catch {
    moduleUnavailable.value = true
    return
  }
  chat.connectWS(portal.username!)
  if (chat.channels.length > 0) {
    await selectTarget('group', chat.channels[0])
  }
})

onBeforeUnmount(() => {
  chat.disconnectWS()
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
            # {{ c }}
          </button>
        </li>
      </ul>

      <h2>Direct Messages</h2>
      <ul class="target-list">
        <li v-for="u in chat.users" :key="u.username">
          <button :class="{ active: active?.kind === 'dm' && active.name === u.username }" @click="selectTarget('dm', u.username)">
            <span class="dot" :class="{ online: u.online }"></span>
            {{ u.username }}
          </button>
        </li>
      </ul>
      <p v-if="chat.channels.length === 0 && chat.users.length === 0" class="hint">Nothing to show yet.</p>
    </aside>

    <main class="thread-pane">
      <div v-if="!active" class="empty-state">
        <p>Pick a channel or a person to start chatting.</p>
      </div>
      <template v-else>
        <div class="thread-header">
          <h1 v-if="active.kind === 'group'"># {{ active.name }}</h1>
          <h1 v-else>
            <span class="dot" :class="{ online: chat.isOnline(active.name) }"></span>
            {{ active.name }}
          </h1>
        </div>

        <div ref="threadEl" class="thread">
          <div v-for="m in messages" :key="m.id" class="message" :class="{ mine: m.sender_username === portal.username }">
            <div class="message-meta">
              <strong>{{ m.sender_username }}</strong>
              <span class="hint-inline">{{ formatTime(m.created_at) }}</span>
            </div>
            <p v-if="m.content">{{ m.content }}</p>
            <a v-if="m.attachment" :href="`/api/portal/chat/attachments/${m.attachment.id}`" target="_blank" rel="noopener" class="attachment-link">
              📎 {{ m.attachment.filename }} ({{ Math.ceil(m.attachment.size_bytes / 1024) }} KB)
            </a>
          </div>
          <p v-if="messages.length === 0" class="hint">No messages yet — say hello.</p>
        </div>

        <form class="composer" @submit.prevent="send">
          <button type="button" :disabled="sending" @click="fileInput?.click()">📎</button>
          <input ref="fileInput" type="file" hidden @change="pickFile" />
          <input v-model="draft" type="text" placeholder="Type a message…" :disabled="sending" />
          <button type="submit" :disabled="sending || !draft.trim()">Send</button>
        </form>
      </template>
    </main>
  </div>
</template>

<style scoped>
.chat-layout {
  display: flex;
  gap: 1.5rem;
  align-items: flex-start;
  height: calc(100vh - 6rem);
}
.sidebar {
  width: 16rem;
  flex-shrink: 0;
  border-right: 1px solid #333;
  padding-right: 1rem;
  overflow-y: auto;
  height: 100%;
}
.sidebar h2 {
  font-size: 0.85rem;
  text-transform: uppercase;
  opacity: 0.7;
  margin: 1rem 0 0.35rem;
}
.sidebar h2:first-child {
  margin-top: 0;
}
.target-list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.target-list button {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  width: 100%;
  text-align: left;
  background: none;
  border: none;
  padding: 0.3rem 0.4rem;
  cursor: pointer;
  color: inherit;
  border-radius: 4px;
  font-size: 0.9rem;
}
.target-list button:hover {
  background: rgba(255, 255, 255, 0.08);
}
.target-list button.active {
  background: rgba(255, 255, 255, 0.08);
  font-weight: bold;
}
.dot {
  width: 0.5rem;
  height: 0.5rem;
  border-radius: 50%;
  background: #555;
  flex-shrink: 0;
}
.dot.online {
  background: #1a7f37;
}
.thread-pane {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  height: 100%;
}
.thread-header h1 {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 1.2rem;
  margin: 0 0 0.5rem;
}
.thread {
  flex: 1;
  overflow-y: auto;
  border: 1px solid #333;
  border-radius: 8px;
  padding: 0.75rem;
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
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
}
.message.mine .message-meta {
  justify-content: flex-end;
}
.message p {
  margin: 0.15rem 0 0;
}
.attachment-link {
  display: inline-block;
  margin-top: 0.15rem;
  font-size: 0.9rem;
}
.composer {
  display: flex;
  gap: 0.5rem;
  margin-top: 0.75rem;
}
.composer input[type='text'] {
  flex: 1;
}
.empty-state {
  opacity: 0.7;
}
.hint {
  font-size: 0.9rem;
  opacity: 0.7;
}
.hint-inline {
  font-size: 0.8rem;
  opacity: 0.6;
}
</style>
