import { defineStore } from 'pinia'
import { api, ApiError } from '../api/client'
import type { PortalMe } from '../api/types'

// Employee portal's own auth — a separate LDAP-backed login and session
// cookie (itp_employee_session) from the admin's (itp_session). Deliberately
// kept apart: a regular employee should never end up with admin access just
// by logging into the wiki.
export const usePortalStore = defineStore('portal', {
  state: () => ({
    username: null as string | null,
    group: '',
    checked: false,
  }),
  actions: {
    async fetchMe() {
      try {
        const res = await api.get<PortalMe>('/portal/me')
        this.username = res.username
        this.group = res.group
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) {
          this.username = null
        } else {
          throw e
        }
      } finally {
        this.checked = true
      }
    },
    async login(username: string, password: string) {
      const res = await api.post<{ username: string }>('/portal/login', { username, password })
      this.username = res.username
      await this.fetchMe()
    },
    async logout() {
      await api.post('/portal/logout')
      this.username = null
      this.group = ''
    },
  },
})
