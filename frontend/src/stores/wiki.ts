import { defineStore } from 'pinia'
import { api } from '../api/client'
import type { WikiAttachment, WikiPage, WikiPageSummary, WikiRecent, WikiRevision } from '../api/types'

export const useWikiStore = defineStore('wiki', {
  state: () => ({
    pages: [] as WikiPageSummary[],
    current: null as WikiPage | null,
    revisions: [] as WikiRevision[],
    attachments: [] as WikiAttachment[],
    recent: [] as WikiRecent[],
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
    // baseRevision: 0 creates a new page (refused with 409 if the path is
    // taken); the page's revision_id edits it (409 if someone saved since).
    async savePage(path: string, title: string, content: string, baseRevision?: number) {
      const body = { path, title, content, ...(baseRevision !== undefined ? { base_revision: baseRevision } : {}) }
      const res = await api.post<{ success: boolean; id: number; revision_id: number }>('/portal/wiki/page', body)
      await this.fetchPages()
      return res.id
    },
    async fetchRecent() {
      const res = await api.get<{ pages: WikiRecent[] }>('/portal/wiki/recent')
      this.recent = res.pages ?? []
    },
    async deleteAttachment(path: string, id: number) {
      await api.delete(`/portal/wiki/attachments/${id}`)
      await this.fetchAttachments(path)
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
