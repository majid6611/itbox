import { defineStore } from 'pinia'
import { api } from '../api/client'
import type {
  CheckUpdatesResponse,
  Manifest,
  ModuleLink,
  ModuleStatus,
  ModulesResponse,
  ModuleUpdate,
} from '../api/types'

const POLL_INTERVAL_MS = 3000

let pollTimer: ReturnType<typeof setInterval> | undefined

export const useModulesStore = defineStore('modules', {
  state: () => ({
    catalog: [] as Manifest[],
    statuses: {} as Record<string, ModuleStatus>,
    links: {} as Record<string, ModuleLink[]>,
    updates: {} as Record<string, ModuleUpdate>,
    loading: false,
    checkingUpdates: false,
  }),
  actions: {
    async fetchAll() {
      this.loading = true
      try {
        const res = await api.get<ModulesResponse>('/modules')
        this.catalog = res.catalog
        this.statuses = Object.fromEntries(res.statuses.map((s) => [s.module_id, s]))
        this.links = res.links
        this.updates = res.updates
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
    async checkForUpdates() {
      this.checkingUpdates = true
      try {
        const res = await api.post<CheckUpdatesResponse>('/modules/check-updates')
        this.updates = Object.fromEntries(res.updates.map((u) => [u.module_id, u]))
        // A brand-new module's files aren't reflected in the catalog we
        // already loaded — refresh it so it shows up without a manual
        // page reload.
        if (res.updates.some((u) => u.new)) {
          await this.fetchAll()
        }
      } finally {
        this.checkingUpdates = false
      }
    },
    async applyUpdate(id: string) {
      await api.post(`/modules/${id}/update`)
      await this.fetchAll()
    },
  },
})
