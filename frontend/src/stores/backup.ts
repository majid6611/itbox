import { defineStore } from 'pinia'
import { api } from '../api/client'
import type { BackupConfig, BackupRun } from '../api/types'

export const useBackupStore = defineStore('backup', {
  state: () => ({
    config: null as BackupConfig | null,
    runs: [] as BackupRun[],
    loading: false,
  }),
  actions: {
    async fetchAll() {
      this.loading = true
      try {
        const [config, history] = await Promise.all([
          api.get<BackupConfig>('/backup/config'),
          api.get<{ runs: BackupRun[] }>('/backup/history'),
        ])
        this.config = config
        this.runs = history.runs ?? []
      } finally {
        this.loading = false
      }
    },
    async saveConfig(config: BackupConfig) {
      await api.post('/backup/config', config)
      await this.fetchAll()
    },
    async runNow() {
      await api.post('/backup/run')
      await this.fetchAll()
    },
    async restoreNow() {
      await api.post('/backup/restore')
      await this.fetchAll()
    },
  },
})
