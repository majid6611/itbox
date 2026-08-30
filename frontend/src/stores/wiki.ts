import { defineStore } from 'pinia'
import { api } from '../api/client'
import type { WikiAttachment, WikiPage, WikiPageSummary, WikiRevision } from '../api/types'

export const useWikiStore = defineStore('wiki', {
  state: () => ({
    pages: [] as WikiPageSummary[],
    current: null as WikiPage | null,
    revisions: [] as WikiRevision[],
    attachments: [] as WikiAttachment[],
    loading: false,
  }),
  actions: {
    async fetchPages() {
      this.loading = true
      try {
        const res = await api.get<{ pages: WikiPageSummary[] }>('/portal/wiki/pages')
        this.pages = res.pages ?? []
      } finally {
        this.loading = false
      }
    },
    async fetchPage(path: string) {
      this.current = await api.get<WikiPage>(`/portal/wiki/page?path=${encodeURIComponent(path)}`)
    },
    async savePage(path: string, title: string, content: string) {
      const res = await api.post<{ success: boolean; id: number }>('/portal/wiki/page', { path, title, content })
      await this.fetchPages()
      return res.id
    },
    async fetchRevisions(path: string) {
      const res = await api.get<{ revisions: WikiRevision[] }>(`/portal/wiki/page/revisions?path=${encodeURIComponent(path)}`)
      this.revisions = res.revisions ?? []
    },
    async fetchRevisionContent(path: string, id: number) {
      const res = await api.get<{ content: string }>(`/portal/wiki/page/revision?path=${encodeURIComponent(path)}&id=${id}`)
      return res.content
    },
    async fetchAttachments(path: string) {
      const res = await api.get<{ attachments: WikiAttachment[] }>(`/portal/wiki/page/attachments?path=${encodeURIComponent(path)}`)
      this.attachments = res.attachments ?? []
    },
    async uploadAttachment(path: string, file: File) {
      const form = new FormData()
      form.append('path', path)
      form.append('file', file)
      const attachment = await api.upload<WikiAttachment>('/portal/wiki/page/attachments', form)
      await this.fetchAttachments(path)
      return attachment
    },
    async deletePage(path: string) {
      await api.delete(`/portal/wiki/page?path=${encodeURIComponent(path)}`)
      if (this.current?.path === path) this.current = null
      await this.fetchPages()
    },
    async renamePage(oldPath: string, newPath: string) {
      await api.post('/portal/wiki/page/rename', { old_path: oldPath, new_path: newPath })
      await this.fetchPages()
    },
    async search(q: string) {
      if (!q.trim()) return []
      const res = await api.get<{ pages: WikiPageSummary[] }>(`/portal/wiki/search?q=${encodeURIComponent(q)}`)
      return res.pages ?? []
    },
  },
})
