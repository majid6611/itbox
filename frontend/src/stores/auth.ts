import { defineStore } from 'pinia'
import { api, ApiError } from '../api/client'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    email: null as string | null,
    checked: false,
  }),
  actions: {
    async fetchMe() {
      try {
        const res = await api.get<{ email: string }>('/auth/me')
        this.email = res.email
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) {
          this.email = null
        } else {
          throw e
        }
      } finally {
        this.checked = true
      }
    },
    async login(email: string, password: string) {
      const res = await api.post<{ email: string }>('/auth/login', { email, password })
      this.email = res.email
    },
    async logout() {
      await api.post('/auth/logout')
      this.email = null
    },
  },
})
