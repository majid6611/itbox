import { defineStore } from 'pinia'
import { api } from '../api/client'
import type { SettingsResponse } from '../api/types'

export const useSettingsStore = defineStore('settings', {
  state: () => ({
    baseDomain: '',
    loading: false,
  }),
  actions: {
    async fetch() {
      this.loading = true
      try {
        const res = await api.get<SettingsResponse>('/settings')
        this.baseDomain = res.base_domain
      } finally {
        this.loading = false
      }
    },
    async save(baseDomain: string) {
      const res = await api.post<SettingsResponse>('/settings', { base_domain: baseDomain })
      this.baseDomain = res.base_domain
    },
  },
})
