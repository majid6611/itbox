import { defineStore } from 'pinia'
import { api } from '../api/client'
import type {
  CalendarEvent,
  CalendarEventsResponse,
  CalendarKind,
  CalendarSettings,
  DirectoryUser,
} from '../api/types'

export interface EventInput {
  calendar: CalendarKind
  title: string
  description: string
  start: string
  end: string
  all_day: boolean
  attendees: string[]
  video_call_url: string
}

export const useCalendarStore = defineStore('calendar', {
  state: () => ({
    available: true,
    events: [] as CalendarEvent[],
    settings: null as CalendarSettings | null,
    directoryUsers: [] as DirectoryUser[],
    loading: false,
  }),
  actions: {
    async fetchSettings() {
      this.settings = await api.get<CalendarSettings>('/portal/calendar/settings')
    },
    async fetchDirectoryUsers() {
      const res = await api.get<{ users: DirectoryUser[] }>('/portal/directory/users')
      this.directoryUsers = res.users
    },
    // FullCalendar tells us the visible range on every navigation
    // (month/week change) — re-fetching for just that window each time,
    // rather than "everything ever," is the entire reason the backend's
    // own CalDAV query is time-ranged in the first place.
    async fetchRange(start: string, end: string) {
      this.loading = true
      try {
        const res = await api.get<CalendarEventsResponse>(
          `/portal/calendar/events?start=${encodeURIComponent(start)}&end=${encodeURIComponent(end)}`,
        )
        this.available = res.available
        this.events = res.events
      } finally {
        this.loading = false
      }
    },
    async createEvent(input: EventInput) {
      const res = await api.post<{ uid: string }>('/portal/calendar/events', input)
      return res.uid
    },
    async updateEvent(calendar: CalendarKind, uid: string, input: EventInput) {
      await api.patch(`/portal/calendar/events/${calendar}/${uid}`, input)
    },
    async deleteEvent(calendar: CalendarKind, uid: string) {
      await api.delete(`/portal/calendar/events/${calendar}/${uid}`)
    },
  },
})
