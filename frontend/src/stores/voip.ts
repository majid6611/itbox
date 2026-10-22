import { defineStore } from 'pinia'
import { api } from '../api/client'
import type { PhoneCall, PhoneExtension, PhoneSettings } from '../api/types'

export const useVoipStore = defineStore('voip', {
  state: () => ({
    pbxOnline: false,
    serverAddress: '',
    extensions: [] as PhoneExtension[],
    settings: null as PhoneSettings | null,
    calls: [] as PhoneCall[],
    loading: false,
  }),
  actions: {
    async fetchAll() {
      this.loading = true
      try {
        await Promise.all([this.fetchExtensions(), this.fetchSettings(), this.fetchCalls()])
      } finally {
        this.loading = false
      }
    },
    async fetchExtensions() {
      const res = await api.get<{ pbx_online: boolean; server_address: string; extensions: PhoneExtension[] }>(
        '/voip/extensions',
      )
      this.pbxOnline = res.pbx_online
      this.serverAddress = res.server_address
      this.extensions = res.extensions ?? []
    },
    async fetchSettings() {
      this.settings = await api.get<PhoneSettings>('/voip/settings')
    },
    async fetchCalls() {
      const res = await api.get<{ calls: PhoneCall[] }>('/voip/calls')
      this.calls = res.calls ?? []
    },
    // Returns the created extension, password included, so the UI can
    // show its setup details right away.
    async create(input: { number: string; name: string; username: string }) {
      const ext = await api.post<PhoneExtension>('/voip/extensions', input)
      await this.fetchExtensions()
      return ext
    },
    async update(ext: PhoneExtension, changes: { in_main_group: boolean }) {
      await api.patch(`/voip/extensions/${ext.number}`, {
        name: ext.name,
        username: ext.username ?? '',
        in_main_group: changes.in_main_group,
      })
      await this.fetchExtensions()
    },
    async resetPassword(number: string) {
      await api.post(`/voip/extensions/${number}/reset-password`)
      await this.fetchExtensions()
    },
    async remove(number: string) {
      await api.delete(`/voip/extensions/${number}`)
      await this.fetchExtensions()
    },
    async saveSettings(input: {
      trunk_enabled: boolean
      trunk_host: string
      trunk_username: string
      trunk_password: string
      trunk_caller_id: string
      allow_international: boolean
    }) {
      await api.put('/voip/settings', input)
      await this.fetchSettings()
    },
  },
})
