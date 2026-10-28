import { defineStore } from 'pinia'
import { api } from '../api/client'
import type { ChatCustomGroup, ChatEvent, ChatMessage, ChatUser } from '../api/types'

export type TargetKind = 'group' | 'dm' | 'custom'

// Matches the server's history page size — a shorter page means no more.
const PAGE_SIZE = 50

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
    // Unread count per thread. Loaded from the server (read state is stored
    // there, so it survives a reload) and bumped live by the WS handler.
    unread: {} as Record<string, number>,
    // Whether a thread may have older messages than the ones loaded.
    hasMore: {} as Record<string, boolean>,
    // Who is typing where: thread key -> username -> when that notice
    // expires (ms). A notice lapses on its own; nobody sends "stopped".
    typing: {} as Record<string, Record<string, number>>,
    lastTypingSent: 0,
    ws: null as WebSocket | null,
    wsConnected: false,
  }),
  getters: {
    hasUnread: (state) => Object.values(state.unread).some((n) => n > 0),
    totalUnread: (state) => Object.values(state.unread).reduce((a, n) => a + n, 0),
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
      const res = await api.get<{ messages: ChatMessage[] }>(`/portal/chat/messages?${this.targetParam(kind, name)}`)
      const key = targetKey(kind, name)
      this.messagesByTarget[key] = res.messages ?? []
      this.hasMore[key] = (res.messages?.length ?? 0) >= PAGE_SIZE
    },
    targetParam(kind: TargetKind, name: string | number) {
      return kind === 'group' ? `group=${encodeURIComponent(name)}` : kind === 'custom' ? `custom_group=${name}` : `with=${encodeURIComponent(String(name))}`
    },
    // Scroll-up paging: the page of messages just older than what's loaded.
    async fetchOlder(kind: TargetKind, name: string | number) {
      const key = targetKey(kind, name)
      const current = this.messagesByTarget[key] ?? []
      if (current.length === 0) return
      const res = await api.get<{ messages: ChatMessage[] }>(
        `/portal/chat/messages?${this.targetParam(kind, name)}&before=${current[0].id}`,
      )
      const older = res.messages ?? []
      this.messagesByTarget[key] = [...older, ...current]
      this.hasMore[key] = older.length >= PAGE_SIZE
    },
    async fetchUnread() {
      const res = await api.get<{ counts: Record<string, number> }>('/portal/chat/unread')
      this.unread = res.counts ?? {}
    },
    // Tell the server a thread is read up to its newest loaded message, and
    // clear the local count. Read state is stored per person on the server,
    // so another tab or device (and a reload) agree on it.
    markRead(key: string) {
      this.unread[key] = 0
      const list = this.messagesByTarget[key]
      const last = list?.[list.length - 1]
      if (last) api.post('/portal/chat/read', { key, message_id: last.id }).catch(() => {})
    },
    // After a dropped connection: fetch whatever was sent while we were
    // away for every thread already loaded (the live push is only a
    // courtesy — Postgres is the source of truth), and refresh unread
    // counts, group list and presence, which may all have changed too.
    async backfill() {
      await Promise.all([
        ...Object.entries(this.messagesByTarget).map(async ([key, list]) => {
          const last = list[list.length - 1]
          if (!last) return
          const i = key.indexOf(':')
          const kind = key.slice(0, i) as TargetKind
          const name = key.slice(i + 1)
          const res = await api.get<{ messages: ChatMessage[] }>(
            `/portal/chat/messages?${this.targetParam(kind, name)}&after=${last.id}`,
          )
          for (const m of res.messages ?? []) this.addMessage(m)
        }),
        this.fetchUnread(),
        this.fetchCustomGroups(),
        this.fetchUsers(),
      ]).catch(() => {})
      if (this.activeKey) this.markRead(this.activeKey)
    },
    // The one place a new message enters a thread. Skips ids already there,
    // since a backfill and a live push can both deliver the same message.
    addMessage(m: ChatMessage) {
      const key = keyForMessage(m, this.myUsername)
      const list = this.messagesByTarget[key]
      // A thread that hasn't been opened has no list yet; its history is
      // fetched in full when it is, so don't start it with just this one
      // message (opening it would then show only that).
      if (!list) return true
      if (list.some((x) => x.id === m.id)) return false
      list.push(m)
      return true
    },
    async sendMessage(kind: TargetKind, name: string | number, content: string, replyToId?: number) {
      const target =
        kind === 'group' ? { group_name: name } : kind === 'custom' ? { custom_group_id: name } : { recipient_username: name }
      const body = { ...target, content, ...(replyToId ? { reply_to_id: replyToId } : {}) }
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
    async leaveGroup(groupId: number, username: string) {
      await api.delete(`/portal/chat/groups/${groupId}/members/${encodeURIComponent(username)}`)
      await this.fetchCustomGroups()
    },
    async search(q: string): Promise<ChatMessage[]> {
      const res = await api.get<{ messages: ChatMessage[] }>(`/portal/chat/search?q=${encodeURIComponent(q)}`)
      return res.messages ?? []
    },
    // Make sure a message is in the loaded part of its thread, paging
    // backwards until it is (search results can be arbitrarily old).
    async loadUntil(kind: TargetKind, name: string | number, id: number) {
      const key = targetKey(kind, name)
      if (!this.messagesByTarget[key]) await this.fetchHistory(kind, name)
      for (let i = 0; i < 40; i++) {
        const list = this.messagesByTarget[key]
        if (!list.length || list[0].id <= id || !this.hasMore[key]) return
        await this.fetchOlder(kind, name)
      }
    },
    // Tell the others in the open thread that we're typing — at most one
    // notice every few seconds, which the receiving side lets lapse.
    sendTyping(kind: TargetKind, name: string | number) {
      const now = Date.now()
      if (!this.ws || this.ws.readyState !== WebSocket.OPEN || now - this.lastTypingSent < 3000) return
      this.lastTypingSent = now
      const target =
        kind === 'group' ? { group_name: name } : kind === 'custom' ? { custom_group_id: name } : { recipient_username: name }
      this.ws.send(JSON.stringify({ type: 'typing', ...target }))
    },
    typingIn(key: string, now: number): string[] {
      const t = this.typing[key] ?? {}
      return Object.keys(t).filter((u) => t[u] > now)
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
      this.markRead(this.activeKey)
    },

    // The chat page was left — nothing is "the open thread" any more, so
    // new messages count as unread instead of being marked read unseen.
    clearActive() {
      this.activeKey = null
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
        this.backfill()
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
        } else if (event.type === 'typing' && event.typing) {
          const t = event.typing
          const key = keyForMessage(
            { sender_username: t.username, group_name: t.group_name, recipient_username: t.recipient_username, custom_group_id: t.custom_group_id } as ChatMessage,
            this.myUsername,
          )
          if (!this.typing[key]) this.typing[key] = {}
          this.typing[key][t.username] = Date.now() + 5000
        } else if (event.type === 'group_invite' || event.type === 'group_changed') {
          // Being added to a group is otherwise silent until a reload —
          // same "went stale without a refresh" gap a missed message
          // would have, just for group membership instead of content.
          this.fetchCustomGroups()
        } else if (event.type === 'message' && event.message) {
          const m = event.message
          const key = keyForMessage(m, this.myUsername)
          if (!this.addMessage(m)) return
          if (this.typing[key]) delete this.typing[key][m.sender_username]
          if (m.sender_username === this.myUsername) return
          // A message in the thread you're looking at is read on arrival; in
          // any other thread (or a background tab) it's unread, visible in
          // the sidebar without needing a refresh to notice.
          if (key === this.activeKey && document.visibilityState === 'visible') {
            this.markRead(key)
          } else {
            this.unread[key] = (this.unread[key] ?? 0) + 1
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
      this.unread = {}
      this.ws?.close()
      this.ws = null
    },
  },
})
