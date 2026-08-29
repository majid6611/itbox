import { defineStore } from 'pinia'
import { api } from '../api/client'
import type { EnableVpnResponse, VpnUsersResponse } from '../api/types'

export const useVpnStore = defineStore('vpn', {
  state: () => ({
    available: false,
    domainConfigured: false,
    users: [] as VpnUsersResponse['users'],
    loading: false,
  }),
  actions: {
    async fetchAll() {
      this.loading = true
      try {
        const res = await api.get<VpnUsersResponse>('/vpn/users')
        this.available = res.available
        this.domainConfigured = res.domain_configured
        this.users = res.users
      } finally {
        this.loading = false
      }
    },
    // Returns the setup key, so the UI can show it once alongside the download link.
    async enable(username: string) {
      const res = await api.post<EnableVpnResponse>(`/vpn/users/${username}/enable`)
      await this.fetchAll()
      return res.setup_key
    },
    async disable(username: string) {
      await api.post(`/vpn/users/${username}/disable`)
      await this.fetchAll()
    },
  },
})
