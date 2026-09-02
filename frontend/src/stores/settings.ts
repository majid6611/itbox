import { defineStore } from 'pinia'
import { api } from '../api/client'
import type { SettingsResponse, ThemeName } from '../api/types'
import { useThemeStore } from './theme'

export const useSettingsStore = defineStore('settings', {
  state: () => ({
    baseDomain: '',
    theme: 'slate' as ThemeName,
    platformVersion: '',
    loading: false,
  }),
  actions: {
    async fetch() {
      this.loading = true
      try {
        const res = await api.get<SettingsResponse>('/settings')
        this.baseDomain = res.base_domain
        this.theme = res.theme
        this.platformVersion = res.platform_version
      } finally {
        this.loading = false
      }
    },
    async save(baseDomain: string, theme: ThemeName) {
      const res = await api.post<SettingsResponse>('/settings', { base_domain: baseDomain, theme })
      this.baseDomain = res.base_domain
      this.theme = res.theme
      // Applies to this tab immediately — no reload needed to see your own change.
      useThemeStore().apply(res.theme)
    },
  },
})
