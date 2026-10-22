import { defineStore } from 'pinia'
import { api } from '../api/client'
import type { MyPhone, PhoneDirectoryEntry } from '../api/types'

export const usePhoneStore = defineStore('phone', {
  state: () => ({
    mine: null as MyPhone | null,
    serverAddress: '',
    directory: [] as PhoneDirectoryEntry[],
    loaded: false,
  }),
  actions: {
    async fetchAll() {
      try {
        const [me, dir] = await Promise.all([
          api.get<{ extension: MyPhone | null; server_address: string }>('/portal/voip/me'),
          api.get<{ entries: PhoneDirectoryEntry[] }>('/portal/voip/directory'),
        ])
        this.mine = me.extension
        this.serverAddress = me.server_address
        this.directory = dir.entries ?? []
      } finally {
        this.loaded = true
      }
    },
  },
})
