import { defineStore } from 'pinia'
import { api } from '../api/client'
import type { CompanyGroup, GroupsResponse } from '../api/types'

export const useGroupsStore = defineStore('groups', {
  state: () => ({
    available: false,
    groups: [] as CompanyGroup[],
    loading: false,
  }),
  actions: {
    async fetchAll() {
      this.loading = true
      try {
        const res = await api.get<GroupsResponse>('/groups')
        this.available = res.available
        this.groups = res.groups
      } finally {
        this.loading = false
      }
    },
    async create(name: string) {
      await api.post('/groups', { name })
      await this.fetchAll()
    },
    async remove(name: string) {
      await api.delete(`/groups/${name}`)
      await this.fetchAll()
    },
  },
})
