import { defineStore } from 'pinia'
import { api } from '../api/client'
import type { CalendarDefaultView, CalendarSettings } from '../api/types'

export const useCalendarAdminStore = defineStore('calendarAdmin', {
  state: () => ({
    settings: null as CalendarSettings | null,
    loading: false,
  }),
  actions: {
    async fetch() {
      this.loading = true
      try {
        this.settings = await api.get<CalendarSettings>('/calendar/settings')
      } finally {
        this.loading = false
      }
    },
    async saveHours(
      startTime: string,
      endTime: string,
      slotDurationMinutes: number,
      defaultView: CalendarDefaultView,
    ) {
      await api.put('/calendar/settings', {
        start_time: startTime,
        end_time: endTime,
        slot_duration_minutes: slotDurationMinutes,
        default_view: defaultView,
      })
      await this.fetch()
    },
    async setMemberColor(username: string, color: string) {
      await api.put(`/calendar/settings/colors/${username}`, { color })
      await this.fetch()
    },
  },
})
