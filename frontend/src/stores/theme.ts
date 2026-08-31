import { defineStore } from 'pinia'
import { api } from '../api/client'
import type { ThemeName, ThemeResponse } from '../api/types'

// The active theme is platform-wide and admin-selected (see Settings), not
// a per-visitor preference — so this just applies whatever the server says
// on every load, no local override. Unauthenticated on purpose: the login
// screens and the employee portal both need it before there's any session.
export const useThemeStore = defineStore('theme', {
  state: () => ({
    theme: 'slate' as ThemeName,
  }),
  actions: {
    async fetch() {
      try {
        const res = await api.get<ThemeResponse>('/theme')
        this.apply(res.theme)
      } catch {
        // Default (slate, already painted via :root) stands if this fails
        // — a themeless app is a bug, a wrong-color one for a moment isn't.
      }
    },
    apply(theme: ThemeName) {
      this.theme = theme
      document.documentElement.dataset.theme = theme
    },
  },
})
