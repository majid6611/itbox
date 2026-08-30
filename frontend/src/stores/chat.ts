import { defineStore } from 'pinia'
import { api } from '../api/client'
import type { ChatCustomGroup, ChatEvent, ChatMessage, ChatUser } from '../api/types'

export type TargetKind = 'group' | 'dm' | 'custom'

// A DM thread, an LDAP-group channel, and a private custom group are all
// just "a target" — this key scheme is how the store tells them apart
// without needing three parallel sets of state everywhere.
function targetKey(kind: TargetKind, name: string | number) {
  return `${kind}:${name}`
}

// Which target a just-arrived message actually belongs to — for a DM this
// depends on which side of it you're on (the thread is keyed by the
// *other* person, not by "recipient", since either party could be either).
function keyForMessage(m: ChatMessage, myUsername: string): string {
  if (m.group_name) return targetKey('group', m.group_name)
  if (m.custom_group_id) return targetKey('custom', m.custom_group_id)
  const other = m.sender_username === myUsername ? m.recipient_username! : m.sender_username
  return targetKey('dm', other)
}

export const useChatStore = defineStore('chat', {
  state: () => ({
    myUsername: '',
    channels: [] as string[],
    users: [] as ChatUser[],
    customGroups: [] as ChatCustomGroup[],
    messagesByTarget: {} as Record<string, ChatMessage[]>,
    // Which thread the UI currently has open — kept here, not just in the
    // component, so the WS handler below knows whether an incoming
    // message should be marked unread or is already being looked at.
    activeKey: null as string | null,
    unread: {} as Record<string, boolean>,
    ws: null as WebSocket | null,
    wsConnected: false,
  }),
  getters: {
    hasUnread: (state) => Object.values(state.unread).some(Boolean),
  },
  actions: {
    async fetchChannels() {
      const res = await api.get<{ channels: string[] }>('/portal/chat/channels')
      this.channels = res.channels ?? []
    },
    async fetchUsers() {
      const res = await api.get<{ users: ChatUser[] }>('/portal/chat/users')
      this.users = res.users ?? []
    },
    async fetchCustomGroups() {
      const res = await api.get<{ groups: ChatCustomGroup[] }>('/portal/chat/groups')
      this.customGroups = res.groups ?? []
    },
    async createGroup(name: string, members: string[]) {
      await api.post('/portal/chat/groups', { name, members })
      await this.fetchCustomGroups()
    },
    async addGroupMember(groupId: number, username: string) {
      await api.post(`/portal/chat/groups/${groupId}/members`, { username })
      await this.fetchCustomGroups()
    },
    async fetchHistory(kind: TargetKind, name: string | number) {
      const param = kind === 'group' ? `group=${encodeURIComponent(name)}` : kind === 'custom' ? `custom_group=${name}` : `with=${encodeURIComponent(String(name))}`
      const res = await api.get<{ messages: ChatMessage[] }>(`/portal/chat/messages?${param}`)
      this.messagesByTarget[targetKey(kind, name)] = res.messages ?? []
    },
    async sendMessage(kind: TargetKind, name: string | number, content: string) {
      const body =
        kind === 'group' ? { group_name: name, content } : kind === 'custom' ? { custom_group_id: name, content } : { recipient_username: name, content }
      // Not appended locally here on purpose — the server echoes every
      // sent message back over the WebSocket (group/custom group: to
      // everyone who can see it, DM: to both participants, including the
      // sender's own other tabs), so the WS handler below is the single
      // place messages get added. Avoids ever showing a message twice.
      await api.post('/portal/chat/messages', body)
    },
    async sendFile(kind: TargetKind, name: string | number, file: File, caption: string) {
      const form = new FormData()
      if (kind === 'group') form.append('group_name', String(name))
      else if (kind === 'custom') form.append('custom_group_id', String(name))
      else form.append('recipient_username', String(name))
      form.append('caption', caption)
      form.append('file', file)
      await api.upload('/portal/chat/attachments', form)
    },
    async editMessage(id: number, content: string) {
      // Not applied locally here either, same reasoning as sendMessage —
      // the server echoes the edit back as a message_updated event to
      // everyone who can see the thread, including the sender's own other
      // tabs, so there's one path that mutates messagesByTarget, not two.
      await api.patch(`/portal/chat/messages/${id}`, { content })
    },
    async deleteMessage(id: number) {
      await api.delete(`/portal/chat/messages/${id}`)
    },
    messagesFor(kind: TargetKind, name: string | number): ChatMessage[] {
      return this.messagesByTarget[targetKey(kind, name)] ?? []
    },
    isOnline(username: string): boolean {
      return this.users.find((u) => u.username === username)?.online ?? false
    },
    // Call when the UI switches to a thread — clears its unread flag and
    // tells the WS handler this is now the one being looked at.
    setActive(kind: TargetKind, name: string | number) {
      this.activeKey = targetKey(kind, name)
      this.unread[this.activeKey] = false
    },

    // Browser push notifications are opt-in and only for when the tab is
    // in the background — a foreground tab already shows the unread dot
    // and, if it's the open thread, the message itself, so notifying too
    // would just be noise. Call from a real user gesture (e.g. clicking
    // an "enable notifications" toggle), not on page load, since browsers
    // ignore/penalize permission prompts that aren't user-initiated.
    async requestNotificationPermission() {
      if (!('Notification' in window)) return
      await Notification.requestPermission()
    },
    notifyNewMessage(m: ChatMessage) {
      if (!('Notification' in window) || Notification.permission !== 'granted') return
      if (document.visibilityState === 'visible') return
      const title = m.group_name ? `#${m.group_name}` : m.sender_username
      const body = m.attachment ? `📎 ${m.attachment.filename}` : m.content
      const n = new Notification(title, { body, tag: `chat-${m.sender_username}-${m.group_name ?? ''}` })
      n.onclick = () => {
        window.focus()
        n.close()
      }
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
        } else if (event.type === 'group_invite') {
          // Being added to a group is otherwise silent until a reload —
          // same "went stale without a refresh" gap a missed message
          // would have, just for group membership instead of content.
          this.fetchCustomGroups()
        } else if (event.type === 'message' && event.message) {
          const m = event.message
          const key = keyForMessage(m, this.myUsername)
          if (!this.messagesByTarget[key]) this.messagesByTarget[key] = []
          this.messagesByTarget[key].push(m)
          // A message for a thread you're not currently looking at should
          // be visible in the sidebar without needing a refresh to notice
          // it happened — that's the whole point of "live", not just
          // delivering bytes into memory nobody's told to look at.
          if (key !== this.activeKey && m.sender_username !== this.myUsername) {
            this.unread[key] = true
            this.notifyNewMessage(m)
          }
        } else if (event.type === 'message_updated' && event.message) {
          // An edit or a delete on a message already in the thread —
          // find it by id and replace it in place, same as any other
          // live-arriving message just without appending.
          const m = event.message
          const key = keyForMessage(m, this.myUsername)
          const list = this.messagesByTarget[key]
          if (list) {
            const i = list.findIndex((x) => x.id === m.id)
            if (i !== -1) list[i] = m
          }
        }
      }
      this.ws = ws
    },
    disconnectWS() {
      this.myUsername = ''
      this.activeKey = null
      this.ws?.close()
      this.ws = null
    },
  },
})
