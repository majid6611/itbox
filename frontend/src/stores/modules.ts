import { defineStore } from 'pinia'
import { api } from '../api/client'
import type { Manifest, ModuleLink, ModuleStatus, ModulesResponse } from '../api/types'

const POLL_INTERVAL_MS = 3000

let pollTimer: ReturnType<typeof setInterval> | undefined

export const useModulesStore = defineStore('modules', {
  state: () => ({
    catalog: [] as Manifest[],
    statuses: {} as Record<string, ModuleStatus>,
    links: {} as Record<string, ModuleLink[]>,
    loading: false,
  }),
  actions: {
    async fetchAll() {
      this.loading = true
      try {
        const res = await api.get<ModulesResponse>('/modules')
        this.catalog = res.catalog
        this.statuses = Object.fromEntries(res.statuses.map((s) => [s.module_id, s]))
        this.links = res.links
      } finally {
        this.loading = false
      }
      this.ensurePolling()
    },
    // Installs run in the background on the server (they can take minutes
    // pulling images on a slow connection) — keep re-fetching status while
    // anything is still "installing" so the UI reflects the real state
    // without the user needing to refresh. Self-stops once nothing is.
    ensurePolling() {
      const anyInstalling = Object.values(this.statuses).some((s) => s.status === 'installing')
      if (anyInstalling && !pollTimer) {
        pollTimer = setInterval(() => this.fetchAll(), POLL_INTERVAL_MS)
      } else if (!anyInstalling && pollTimer) {
        clearInterval(pollTimer)
        pollTimer = undefined
      }
    },
    async install(id: string, config: Record<string, string>) {
      await api.post(`/modules/${id}/install`, config)
      await this.fetchAll()
    },
    async enable(id: string) {
      await api.post(`/modules/${id}/enable`)
      await this.fetchAll()
    },
    async disable(id: string) {
      await api.post(`/modules/${id}/disable`)
      await this.fetchAll()
    },
    async uninstall(id: string) {
      await api.delete(`/modules/${id}`)
      await this.fetchAll()
    },
    async setVisibility(id: string, visibility: 'public' | 'private') {
      await api.post(`/modules/${id}/visibility`, { visibility })
      await this.fetchAll()
    },
  },
})
