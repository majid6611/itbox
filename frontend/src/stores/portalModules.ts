import { defineStore } from 'pinia'
import { api } from '../api/client'

export const usePortalModulesStore = defineStore('portalModules', {
  state: () => ({
    modules: {} as Record<string, boolean>,
    videoCallBaseURL: '',
    calendarAvailable: false,
    calendarCalDAVBaseURL: '',
    checked: false,
  }),
  actions: {
    async fetchAll() {
      try {
        const res = await api.get<{
          modules: Record<string, boolean>
          video_call_base_url?: string
          calendar_available?: boolean
          calendar_caldav_base_url?: string
        }>('/portal/modules')
        this.modules = res.modules ?? {}
        this.videoCallBaseURL = res.video_call_base_url ?? ''
        this.calendarAvailable = res.calendar_available ?? false
        this.calendarCalDAVBaseURL = res.calendar_caldav_base_url ?? ''
      } finally {
        this.checked = true
      }
    },
  },
})
