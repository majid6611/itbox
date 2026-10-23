import { defineStore } from 'pinia'
import { api } from '../api/client'
import type { EnableVpnResponse, VpnUsersResponse } from '../api/types'

export const useVpnStore = defineStore('vpn', {
  state: () => ({
    available: false,
    domainConfigured: false,
    users: [] as VpnUsersResponse['users'],
    devices: [] as VpnUsersResponse['devices'],
    loading: false,
    // Office LAN access through the VPN — see GET /api/vpn/office-network.
    office: { available: false, enabled: false, network: '', ports: '', detected: '' },
    twoFactor: false,
  }),
  actions: {
    async fetchAll() {
      this.loading = true
      try {
        const res = await api.get<VpnUsersResponse>('/vpn/users')
        this.available = res.available
        this.domainConfigured = res.domain_configured
        this.users = res.users
        this.devices = res.devices
      } finally {
        this.loading = false
      }
    },
    // Returns what to show once alongside the download link: a setup key,
    // or (with 2FA on) the person's login email and password.
    async enable(username: string) {
      const res = await api.post<EnableVpnResponse>(`/vpn/users/${username}/enable`)
      await this.fetchAll()
      return res
    },
    async fetchTwoFactor() {
      this.twoFactor = (await api.get<{ enabled: boolean }>('/vpn/two-factor')).enabled
    },
    async saveTwoFactor(enabled: boolean) {
      this.twoFactor = (await api.put<{ enabled: boolean }>('/vpn/two-factor', { enabled })).enabled
    },
    async fetchOfficeNetwork() {
      this.office = await api.get<{ available: boolean; enabled: boolean; network: string; ports: string; detected: string }>(
        '/vpn/office-network',
      )
    },
    async saveOfficeNetwork(enabled: boolean, network: string, ports: string) {
      await api.put('/vpn/office-network', { enabled, network, ports })
      await this.fetchOfficeNetwork()
    },
    async approveDevice(id: string) {
      await api.post(`/vpn/devices/${id}/approve`)
      await this.fetchAll()
    },
    async removeDevice(id: string) {
      await api.delete(`/vpn/devices/${id}`)
      await this.fetchAll()
    },
    async disable(username: string) {
      await api.post(`/vpn/users/${username}/disable`)
      await this.fetchAll()
    },
  },
})
