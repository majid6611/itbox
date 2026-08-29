import { defineStore } from 'pinia'
import { api } from '../api/client'
import type { CompanyUser, CreateUserResponse, ResetPasswordResponse, UsersResponse } from '../api/types'

export const useUsersStore = defineStore('users', {
  state: () => ({
    available: false,
    users: [] as CompanyUser[],
    loading: false,
  }),
  actions: {
    async fetchAll() {
      this.loading = true
      try {
        const res = await api.get<UsersResponse>('/users')
        this.available = res.available
        this.users = res.users
      } finally {
        this.loading = false
      }
    },
    // Returns the password actually set, so the UI can show it once.
    async create(input: { username: string; email: string; name: string; password: string; group: string }) {
      const res = await api.post<CreateUserResponse>('/users', input)
      await this.fetchAll()
      return res.password
    },
    async update(username: string, input: { email: string; name: string }) {
      await api.patch(`/users/${username}`, input)
      await this.fetchAll()
    },
    async changeGroup(username: string, group: string) {
      await api.post(`/users/${username}/group`, { group })
      await this.fetchAll()
    },
    // Returns the new password (generated server-side if none was given).
    async resetPassword(username: string, password: string) {
      const res = await api.post<ResetPasswordResponse>(`/users/${username}/reset-password`, { password })
      return res.password
    },
    async remove(username: string) {
      await api.delete(`/users/${username}`)
      await this.fetchAll()
    },
    async disable(username: string) {
      await api.post(`/users/${username}/disable`)
      await this.fetchAll()
    },
    // Returns the fresh password issued on re-enable (the old one can't
    // be recovered — disabling reset it to something nobody was shown).
    async enable(username: string) {
      const res = await api.post<ResetPasswordResponse>(`/users/${username}/enable`)
      await this.fetchAll()
      return res.password
    },
  },
})
