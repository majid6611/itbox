import { defineStore } from 'pinia'
import { api } from '../api/client'
import type { ChatEvent, ChatMessage, ChatUser } from '../api/types'

// A DM thread and a group channel are both just "a target" — this key
// scheme is how the store tells them apart without needing two parallel
// sets of state everywhere.
function targetKey(kind: 'group' | 'dm', name: string) {
  return `${kind}:${name}`
}

export const useChatStore = defineStore('chat', {
  state: () => ({
    myUsername: '',
    channels: [] as string[],
    users: [] as ChatUser[],
    messagesByTarget: {} as Record<string, ChatMessage[]>,
    ws: null as WebSocket | null,
    wsConnected: false,
  }),
  actions: {
    async fetchChannels() {
      const res = await api.get<{ channels: string[] }>('/portal/chat/channels')
      this.channels = res.channels ?? []
    },
    async fetchUsers() {
      const res = await api.get<{ users: ChatUser[] }>('/portal/chat/users')
      this.users = res.users ?? []
    },
    async fetchHistory(kind: 'group' | 'dm', name: string) {
      const param = kind === 'group' ? `group=${encodeURIComponent(name)}` : `with=${encodeURIComponent(name)}`
      const res = await api.get<{ messages: ChatMessage[] }>(`/portal/chat/messages?${param}`)
      this.messagesByTarget[targetKey(kind, name)] = res.messages ?? []
    },
    async sendMessage(kind: 'group' | 'dm', name: string, content: string) {
      const body = kind === 'group' ? { group_name: name, content } : { recipient_username: name, content }
      // Not appended locally here on purpose — the server echoes every
      // sent message back over the WebSocket (group: to everyone, DM: to
      // both participants, including the sender's own other tabs), so the
      // WS handler below is the single place messages get added. Avoids
      // ever showing a message twice.
      await api.post('/portal/chat/messages', body)
    },
    async sendFile(kind: 'group' | 'dm', name: string, file: File, caption: string) {
      const form = new FormData()
      if (kind === 'group') form.append('group_name', name)
      else form.append('recipient_username', name)
      form.append('caption', caption)
      form.append('file', file)
      await api.upload('/portal/chat/attachments', form)
    },
    messagesFor(kind: 'group' | 'dm', name: string): ChatMessage[] {
      return this.messagesByTarget[targetKey(kind, name)] ?? []
    },
    isOnline(username: string): boolean {
      return this.users.find((u) => u.username === username)?.online ?? false
    },

    connectWS(myUsername: string) {
      this.myUsername = myUsername
      const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
      const ws = new WebSocket(`${proto}//${window.location.host}/api/portal/chat/ws`)
      ws.onopen = () => {
        this.wsConnected = true
      }
      ws.onclose = () => {
        this.wsConnected = false
        // The heartbeat/reconnect story is server-side (see the chat
        // module's hub package) for *detecting* a dead connection: from
        // the browser's side, a dropped connection just needs a plain
        // retry. Simple fixed delay — good enough at this scale, no need
        // for backoff/jitter machinery for a small company's own chat.
        setTimeout(() => {
          if (this.myUsername) this.connectWS(this.myUsername)
        }, 3000)
      }
      ws.onmessage = (ev) => {
        const event: ChatEvent = JSON.parse(ev.data)
        if (event.type === 'presence' && event.presence) {
          const u = this.users.find((x) => x.username === event.presence!.username)
          if (u) u.online = event.presence.online
        } else if (event.type === 'message' && event.message) {
          const m = event.message
          const key = m.group_name
            ? targetKey('group', m.group_name)
            : targetKey('dm', m.sender_username === this.myUsername ? m.recipient_username! : m.sender_username)
          if (!this.messagesByTarget[key]) this.messagesByTarget[key] = []
          this.messagesByTarget[key].push(m)
        }
      }
      this.ws = ws
    },
    disconnectWS() {
      this.myUsername = ''
      this.ws?.close()
      this.ws = null
    },
  },
})
