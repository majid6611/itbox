import { defineStore } from 'pinia'
import { api } from '../api/client'

export const usePortalModulesStore = defineStore('portalModules', {
  state: () => ({
    modules: {} as Record<string, boolean>,
    checked: false,
  }),
  actions: {
    async fetchAll() {
      try {
        const res = await api.get<{ modules: Record<string, boolean> }>('/portal/modules')
        this.modules = res.modules ?? {}
      } finally {
        this.checked = true
      }
    },
  },
})
