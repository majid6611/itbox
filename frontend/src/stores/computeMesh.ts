import { defineStore } from 'pinia'
import { api } from '../api/client'
import type { MeshDevicesResponse } from '../api/types'

export const useComputeMeshStore = defineStore('computeMesh', {
  state: () => ({
    available: false,
    devices: [] as MeshDevicesResponse['devices'],
    loading: false,
  }),
  actions: {
    async fetchAll() {
      this.loading = true
      try {
        const res = await api.get<MeshDevicesResponse>('/compute-mesh/devices')
        this.available = res.available
        this.devices = res.devices
      } finally {
        this.loading = false
      }
    },
    async addDevice(name: string, host: string, amtUsername: string, amtPassword: string) {
      await api.post('/compute-mesh/devices', { name, host, amt_username: amtUsername, amt_password: amtPassword })
      await this.fetchAll()
    },
    async removeDevice(id: number) {
      await api.delete(`/compute-mesh/devices/${id}`)
      await this.fetchAll()
    },
    async power(id: number, action: 'on' | 'off' | 'cycle') {
      await api.post(`/compute-mesh/devices/${id}/power`, { action })
    },
  },
})
