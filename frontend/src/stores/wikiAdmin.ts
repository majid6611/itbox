import { defineStore } from 'pinia'
import { api } from '../api/client'
import type { WikiPageSummary, WikiPermissionRule } from '../api/types'

export const useWikiAdminStore = defineStore('wikiAdmin', {
  state: () => ({
    pages: [] as WikiPageSummary[],
    rules: [] as WikiPermissionRule[],
    loading: false,
  }),
  actions: {
    async fetchPages() {
      this.loading = true
      try {
        const res = await api.get<{ pages: WikiPageSummary[] }>('/wiki/pages')
        this.pages = res.pages ?? []
      } finally {
        this.loading = false
      }
    },
    async fetchRules(path: string) {
      const res = await api.get<{ rules: WikiPermissionRule[] }>(`/wiki/permissions?path=${encodeURIComponent(path)}`)
      this.rules = res.rules ?? []
    },
    async saveRules(path: string, rules: WikiPermissionRule[]) {
      await api.post('/wiki/permissions', { path, rules })
      this.rules = rules
    },
  },
})
