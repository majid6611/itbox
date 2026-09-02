<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import FullCalendar from '@fullcalendar/vue3'
import dayGridPlugin from '@fullcalendar/daygrid'
import timeGridPlugin from '@fullcalendar/timegrid'
import interactionPlugin from '@fullcalendar/interaction'
import type {
  DateSelectArg,
  EventClickArg,
  EventDropArg,
  EventInput as FCEventInput,
} from '@fullcalendar/core'
import type { EventResizeDoneArg } from '@fullcalendar/interaction'
import { useCalendarStore } from '../../stores/calendar'
import { usePortalModulesStore } from '../../stores/portalModules'
import { usePortalStore } from '../../stores/portal'
import type { CalendarKind } from '../../api/types'

const calendar = useCalendarStore()
const portalModules = usePortalModulesStore()
const portal = usePortalStore()

const showSubscribeInfo = ref(false)
// Same path convention as the backend's own radicalefs.CompanyCalendarPath
// / PersonalCalendarPath — these are the exact URLs a phone or desktop
// calendar app subscribes to directly (bypassing this platform's own API
// entirely), using the employee's own portal username and password same
// as logging in here.
const companyCalDAVURL = computed(() => portalModules.calendarCalDAVBaseURL + '/company/calendar/')
const personalCalDAVURL = computed(() =>
  portal.username ? `${portalModules.calendarCalDAVBaseURL}/${portal.username}/personal/` : '',
)

const showForm = ref(false)
const saving = ref(false)
const error = ref('')
const editingUID = ref<string | null>(null)
const editingCalendar = ref<CalendarKind>('company')
const form = reactive({
  calendar: 'company' as CalendarKind,
  title: '',
  description: '',
  start: '',
  end: '',
  allDay: false,
  attendees: [] as string[],
  videoCallURL: '',
})

// Same unguessable-room-id approach as Chat's own "Start call" — a fresh
// random room per event, not tied to anything guessable about the event
// itself.
function randomRoomId() {
  const bytes = new Uint8Array(8)
  crypto.getRandomValues(bytes)
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
}
function addVideoCall() {
  if (!portalModules.videoCallBaseURL) return
  form.videoCallURL = `${portalModules.videoCallBaseURL}/${randomRoomId()}`
}

// Personal events are always teal (it's always "you"). A company event
// shows in whoever created it's own color instead — set per employee on
// the admin's Calendar Settings page — falling back to amber for one
// with no known creator (a native CalDAV client created it, or it
// predates this platform tracking that at all).
const PERSONAL_COLOR = '#0f766e'
const FALLBACK_COMPANY_COLOR = '#b8860b'

function colorFor(kind: CalendarKind, createdBy?: string) {
  if (kind === 'personal') return PERSONAL_COLOR
  if (createdBy && calendar.settings?.member_colors[createdBy]) {
    return calendar.settings.member_colors[createdBy]
  }
  return FALLBACK_COMPANY_COLOR
}

const fcEvents = computed<FCEventInput[]>(() =>
  calendar.events.map((e) => ({
    id: `${e.calendar}:${e.uid}`,
    title: e.title,
    start: e.start,
    end: e.end,
    allDay: e.all_day,
    backgroundColor: colorFor(e.calendar, e.created_by),
    borderColor: colorFor(e.calendar, e.created_by),
    extendedProps: {
      calendar: e.calendar,
      uid: e.uid,
      description: e.description,
      attendees: e.attendees ?? [],
      videoCallURL: e.video_call_url ?? '',
    },
  })),
)

function pad(n: number) {
  return String(n).padStart(2, '0')
}
// Local wall-clock formatting, not toISOString's UTC — a picker showing
// UTC time to someone in another timezone would silently save the wrong
// hour from their point of view.
function toDateLocal(d: Date) {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}
function toDateTimeLocal(d: Date) {
  return `${toDateLocal(d)}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}
// The inverse of toDateLocal — new Date("YYYY-MM-DD") parses as UTC
// midnight per spec, which would shift an all-day event to the wrong
// date for anyone west of UTC, so this builds the local-midnight Date
// directly from the parts instead of trusting the string constructor.
function fromDateLocal(s: string) {
  const [y, m, d] = s.split('-').map(Number)
  return new Date(y, m - 1, d)
}
function addDays(d: Date, n: number) {
  const copy = new Date(d)
  copy.setDate(copy.getDate() + n)
  return copy
}

function resetForm(kind: CalendarKind, start: Date, end: Date, allDay: boolean) {
  form.calendar = kind
  form.title = ''
  form.description = ''
  form.allDay = allDay
  form.start = allDay ? toDateLocal(start) : toDateTimeLocal(start)
  form.end = allDay ? toDateLocal(end) : toDateTimeLocal(end)
  form.attendees = []
  form.videoCallURL = ''
}

// Toggling "All day" needs the existing picked moment reformatted to the
// other input's shape, not reset to nothing.
function onAllDayToggle() {
  const start = form.allDay ? fromDateLocal(form.start.slice(0, 10)) : new Date(form.start)
  const end = form.allDay ? fromDateLocal(form.end.slice(0, 10)) : new Date(form.end)
  form.start = form.allDay ? toDateLocal(start) : toDateTimeLocal(start)
  form.end = form.allDay ? toDateLocal(end) : toDateTimeLocal(end)
}

function openCreateForm(start: Date, end: Date, allDay: boolean) {
  editingUID.value = null
  error.value = ''
  resetForm('company', start, end, allDay)
  showForm.value = true
}

function handleSelect(arg: DateSelectArg) {
  // Clicking a single day in month view is reported as an all-day
  // selection no matter what — that's just how day-grid clicks work, not
  // a signal the event itself should be all-day. Defaulting to a timed
  // 9-10am event there (with "All day" still available to check) is what
  // most clicks actually mean; only a genuine multi-day drag defaults to
  // all-day. Week/day view selections already carry a real time range,
  // so those pass through untouched.
  const spansMultipleDays = arg.allDay && arg.end.getTime() - arg.start.getTime() > 24 * 60 * 60 * 1000
  if (arg.allDay && !spansMultipleDays) {
    const start = new Date(arg.start)
    start.setHours(9, 0, 0, 0)
    const end = new Date(arg.start)
    end.setHours(10, 0, 0, 0)
    openCreateForm(start, end, false)
  } else {
    // FullCalendar's own all-day selections use an exclusive end (the day
    // after the last one picked) — the form shows/sends the last picked
    // day itself, so that has to come off here before it reaches it.
    const formEnd = arg.allDay ? addDays(arg.end, -1) : arg.end
    openCreateForm(arg.start, formEnd, arg.allDay)
  }
}

function handleEventClick(arg: EventClickArg) {
  const kind = arg.event.extendedProps.calendar as CalendarKind
  const uid = arg.event.extendedProps.uid as string
  editingUID.value = uid
  editingCalendar.value = kind
  error.value = ''
  const start = arg.event.start ?? new Date()
  // Same exclusive-end correction as handleSelect — a stored all-day
  // event's end date is one day past the last real day of the event.
  const rawEnd = arg.event.end ?? start
  const end = arg.event.allDay ? addDays(rawEnd, -1) : rawEnd
  resetForm(kind, start, end, arg.event.allDay)
  form.title = arg.event.title
  form.description = (arg.event.extendedProps.description as string) ?? ''
  form.attendees = [...((arg.event.extendedProps.attendees as string[]) ?? [])]
  form.videoCallURL = (arg.event.extendedProps.videoCallURL as string) ?? ''
  showForm.value = true
}

// An all-day event is a calendar date, not a moment in time — sending it
// through Date/toISOString() would convert local midnight to UTC and
// silently roll it onto the wrong day for anyone not exactly at UTC
// (confirmed live: any positive-offset timezone shifts it back a day).
// Building the "Z" string directly from the typed digits, with no real
// timezone conversion involved, keeps the calendar date exactly what was
// picked no matter where the browser is.
function toISO(value: string, allDay: boolean) {
  if (allDay) return `${value}T00:00:00.000Z`
  return new Date(value).toISOString()
}

// The form's End field is the last day the event actually covers (a
// single-day event has the same date in both fields) — but CalDAV/
// FullCalendar both expect an all-day DTEND one day past that (confirmed
// live: sending them equal stores a zero-duration event Radicale's own
// day-view query then never matches, even though month/week view still
// drew a bar for it). This is the one place that exclusive day gets
// added back in before the value leaves the browser.
function toEndISO(value: string, allDay: boolean) {
  if (!allDay) return new Date(value).toISOString()
  return `${toDateLocal(addDays(fromDateLocal(value), 1))}T00:00:00.000Z`
}

async function submitForm() {
  if (!form.title.trim()) {
    error.value = 'Title is required.'
    return
  }
  saving.value = true
  error.value = ''
  try {
    const input = {
      calendar: form.calendar,
      title: form.title.trim(),
      description: form.description.trim(),
      start: toISO(form.start, form.allDay),
      end: toEndISO(form.end, form.allDay),
      all_day: form.allDay,
      attendees: form.attendees,
      video_call_url: form.videoCallURL,
    }
    if (editingUID.value) {
      await calendar.updateEvent(editingCalendar.value, editingUID.value, input)
    } else {
      await calendar.createEvent(input)
    }
    showForm.value = false
    await refetchCurrentRange()
  } catch {
    error.value = 'Could not save this event — try again.'
  } finally {
    saving.value = false
  }
}

// Drag-to-move and drag-to-resize both land here: confirm the change
// against the same event's title, apply it if accepted, or hand the
// event back to its pre-drag position/size (arg.revert()) if declined or
// if the save fails — FullCalendar already moved/resized it optimistically
// by the time either handler runs, so silence on the decline path would
// otherwise leave a change on screen nothing agreed to.
async function applyDragChange(arg: EventDropArg | EventResizeDoneArg, confirmMessage: string) {
  if (!confirm(confirmMessage)) {
    arg.revert()
    return
  }
  const kind = arg.event.extendedProps.calendar as CalendarKind
  const uid = arg.event.extendedProps.uid as string
  const start = arg.event.start ?? new Date()
  const end = arg.event.end ?? start
  // Same floating-date handling as toISO: dragging an all-day event to a
  // new day still just picks a calendar date, not a real moment, so this
  // has to avoid toISOString()'s real timezone conversion the same way.
  const toPayloadISO = (d: Date) => (arg.event.allDay ? `${toDateLocal(d)}T00:00:00.000Z` : d.toISOString())
  try {
    await calendar.updateEvent(kind, uid, {
      calendar: kind,
      title: arg.event.title,
      description: (arg.event.extendedProps.description as string) ?? '',
      start: toPayloadISO(start),
      end: toPayloadISO(end),
      all_day: arg.event.allDay,
      attendees: (arg.event.extendedProps.attendees as string[]) ?? [],
      video_call_url: (arg.event.extendedProps.videoCallURL as string) ?? '',
    })
    await refetchCurrentRange()
  } catch {
    arg.revert()
  }
}

function handleEventDrop(arg: EventDropArg) {
  applyDragChange(arg, `Move "${arg.event.title}" to ${arg.event.start?.toLocaleString() ?? ''}?`)
}

function handleEventResize(arg: EventResizeDoneArg) {
  applyDragChange(arg, `Change "${arg.event.title}"'s duration?`)
}

async function deleteEvent() {
  if (!editingUID.value) return
  if (!confirm(`Delete "${form.title}"?`)) return
  saving.value = true
  try {
    await calendar.deleteEvent(editingCalendar.value, editingUID.value)
    showForm.value = false
    await refetchCurrentRange()
  } catch {
    error.value = 'Could not delete this event — try again.'
  } finally {
    saving.value = false
  }
}

let currentRange: { start: string; end: string } | null = null
async function refetchCurrentRange() {
  if (currentRange) await calendar.fetchRange(currentRange.start, currentRange.end)
}

function handleDatesSet(arg: { startStr: string; endStr: string }) {
  currentRange = { start: arg.startStr, end: arg.endStr }
  calendar.fetchRange(arg.startStr, arg.endStr)
}

const VIEW_NAME = { month: 'dayGridMonth', week: 'timeGridWeek', day: 'timeGridDay' } as const

const calendarOptions = computed(() => ({
  plugins: [dayGridPlugin, timeGridPlugin, interactionPlugin],
  // FullCalendar only reads initialView once, at mount — changing it
  // later on an already-mounted calendar has no effect, so the whole
  // <FullCalendar> element is kept out of the template (see the v-if
  // below) until settings have actually loaded, rather than mounting it
  // early with a guess and hoping this prop updates it later.
  initialView: VIEW_NAME[calendar.settings?.default_view ?? 'month'],
  headerToolbar: { left: 'prev,next today', center: 'title', right: 'dayGridMonth,timeGridWeek,timeGridDay' },
  height: 'auto',
  selectable: true,
  editable: true,
  slotMinTime: (calendar.settings?.start_time ?? '07:00') + ':00',
  slotMaxTime: (calendar.settings?.end_time ?? '20:00') + ':00',
  slotDuration: `00:${String(calendar.settings?.slot_duration_minutes ?? 30).padStart(2, '0')}:00`,
  select: handleSelect,
  eventClick: handleEventClick,
  eventDrop: handleEventDrop,
  eventResize: handleEventResize,
  datesSet: handleDatesSet,
  events: fcEvents.value,
}))

onMounted(() => {
  calendar.fetchSettings()
  calendar.fetchDirectoryUsers()
})

function toggleAttendee(username: string) {
  const i = form.attendees.indexOf(username)
  if (i === -1) form.attendees.push(username)
  else form.attendees.splice(i, 1)
}
</script>

<template>
  <div>
    <h1>Calendar</h1>
    <p class="subtitle">The company calendar, plus your own personal one — click a date to add an event.</p>

    <div v-if="!calendar.available" class="notice">
      The Calendar module isn't available right now — ask an admin to check it in the Module Store.
    </div>

    <template v-else>
      <div class="legend">
        <span class="legend-item"><span class="dot" :style="{ background: PERSONAL_COLOR }"></span> Personal</span>
        <span
          v-for="(color, username) in calendar.settings?.member_colors"
          :key="username"
          class="legend-item"
        >
          <span class="dot" :style="{ background: color }"></span> {{ username }}
        </span>
        <button
          v-if="portalModules.calendarCalDAVBaseURL"
          type="button"
          class="secondary subscribe-toggle"
          @click="showSubscribeInfo = !showSubscribeInfo"
        >
          📱 Subscribe from phone/computer
        </button>
      </div>

      <div v-if="showSubscribeInfo" class="card subscribe-card">
        <p class="hint">
          Add these as CalDAV accounts in your phone or desktop calendar app, using your portal username
          (<strong>{{ portal.username }}</strong>) and the same password you log in here with.
        </p>
        <label class="subscribe-field">
          Company calendar
          <input :value="companyCalDAVURL" type="text" readonly @focus="($event.target as HTMLInputElement).select()" />
        </label>
        <label class="subscribe-field">
          Personal calendar
          <input :value="personalCalDAVURL" type="text" readonly @focus="($event.target as HTMLInputElement).select()" />
        </label>
        <p class="hint">
          If your app refuses a plain http:// address, it needs a real domain with HTTPS set up first — ask an admin.
        </p>
      </div>

      <div class="card calendar-card">
        <FullCalendar v-if="calendar.settings" :options="calendarOptions" />
        <p v-else class="hint">Loading…</p>
      </div>

      <div v-if="showForm" class="modal-backdrop" @click.self="showForm = false">
        <form class="modal card" @submit.prevent="submitForm">
          <h2>{{ editingUID ? 'Edit event' : 'New event' }}</h2>
          <label>
            Calendar
            <select v-model="form.calendar">
              <option value="company">Company (everyone can see and edit)</option>
              <option value="personal">Personal (only you)</option>
            </select>
          </label>
          <label>
            Title
            <input v-model="form.title" type="text" required autofocus />
          </label>
          <label>
            Description
            <textarea v-model="form.description" rows="2"></textarea>
          </label>
          <label class="checkbox">
            <input v-model="form.allDay" type="checkbox" @change="onAllDayToggle" /> All day
          </label>
          <label>
            Start
            <input v-model="form.start" :type="form.allDay ? 'date' : 'datetime-local'" required />
          </label>
          <label>
            End
            <input v-model="form.end" :type="form.allDay ? 'date' : 'datetime-local'" required />
          </label>
          <div v-if="calendar.directoryUsers.length" class="attendees-field">
            <span class="attendees-label">Attendees</span>
            <div class="attendees-list">
              <label v-for="u in calendar.directoryUsers" :key="u.username" class="attendee-option">
                <input
                  type="checkbox"
                  :checked="form.attendees.includes(u.username)"
                  @change="toggleAttendee(u.username)"
                />
                {{ u.name || u.username }}
              </label>
            </div>
          </div>
          <div v-if="portalModules.videoCallBaseURL" class="video-call-field">
            <span class="attendees-label">Video call</span>
            <div v-if="form.videoCallURL" class="video-call-set">
              <a :href="form.videoCallURL" target="_blank" rel="noopener">{{ form.videoCallURL }}</a>
              <button type="button" class="secondary" @click="form.videoCallURL = ''">Remove</button>
            </div>
            <button v-else type="button" class="secondary" @click="addVideoCall">📹 Add video call</button>
          </div>
          <p v-if="error" class="error-message">{{ error }}</p>
          <div class="modal-actions">
            <button type="submit" :disabled="saving">{{ saving ? 'Saving…' : 'Save' }}</button>
            <button type="button" class="secondary" @click="showForm = false">Cancel</button>
            <button v-if="editingUID" type="button" class="danger" :disabled="saving" @click="deleteEvent">Delete</button>
          </div>
        </form>
      </div>
    </template>
  </div>
</template>

<style scoped>
h1 {
  margin-bottom: 0.35rem;
}
.subtitle {
  color: var(--text-dim);
  font-size: 0.92rem;
  margin: 0 0 1.25rem;
}
.hint {
  font-size: 0.85rem;
  color: var(--text-dim);
}
.notice {
  border: 1px solid var(--border);
  background: var(--surface);
  border-radius: 10px;
  padding: 1rem;
  max-width: 32rem;
  color: var(--text-dim);
  font-size: 0.9rem;
}
.legend {
  display: flex;
  align-items: center;
  gap: 1.25rem;
  margin-bottom: 0.85rem;
  font-size: 0.85rem;
  color: var(--text-dim);
}
.legend-item {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
}
.dot {
  width: 9px;
  height: 9px;
  border-radius: 50%;
  display: inline-block;
}
.subscribe-toggle {
  margin-left: auto;
  font-size: 0.82rem;
}
.subscribe-card {
  padding: 1rem 1.2rem;
  margin-bottom: 0.85rem;
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
  max-width: 34rem;
}
.subscribe-field {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  font-size: 0.85rem;
  font-weight: 500;
}
.subscribe-field input {
  font-family: var(--font-mono, monospace);
  font-size: 0.82rem;
}
.calendar-card {
  padding: 1.1rem;
}
.modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.45);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 50;
}
.modal {
  width: 100%;
  max-width: 26rem;
  padding: 1.25rem 1.4rem;
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}
.modal h2 {
  font-size: 1.05rem;
  margin: 0 0 0.25rem;
}
.modal label {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  font-size: 0.85rem;
  font-weight: 500;
}
.modal .checkbox {
  flex-direction: row;
  align-items: center;
  gap: 0.5rem;
}
.attendees-field {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
}
.attendees-label {
  font-size: 0.85rem;
  font-weight: 500;
}
.attendees-list {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  max-height: 8rem;
  overflow-y: auto;
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 0.5rem 0.65rem;
}
.attendees-list .attendee-option {
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.85rem;
  font-weight: 400;
}
.video-call-field {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
}
.video-call-set {
  display: flex;
  align-items: center;
  gap: 0.6rem;
  font-size: 0.85rem;
}
.video-call-set a {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.modal-actions {
  display: flex;
  gap: 0.5rem;
  margin-top: 0.4rem;
}
.error-message {
  color: var(--danger-text);
  font-size: 0.85rem;
  margin: 0;
}
button.danger {
  margin-left: auto;
  color: var(--danger-text);
  border-color: var(--danger-text);
  background: transparent;
}
</style>
