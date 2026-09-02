<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { useCalendarAdminStore } from '../stores/calendarAdmin'
import type { CalendarDefaultView } from '../api/types'

const admin = useCalendarAdminStore()
const hours = reactive({ start: '07:00', end: '20:00', slotMinutes: 30, defaultView: 'month' as CalendarDefaultView })
const savingHours = ref(false)
const savingColor = reactive<Record<string, boolean>>({})

onMounted(async () => {
  await admin.fetch()
})

watch(
  () => admin.settings,
  (s) => {
    if (!s) return
    hours.start = s.start_time
    hours.end = s.end_time
    hours.slotMinutes = s.slot_duration_minutes
    hours.defaultView = s.default_view
  },
  { immediate: true },
)

async function saveHours() {
  savingHours.value = true
  try {
    await admin.saveHours(hours.start, hours.end, hours.slotMinutes, hours.defaultView)
  } finally {
    savingHours.value = false
  }
}

async function setColor(username: string, color: string) {
  savingColor[username] = true
  try {
    await admin.setMemberColor(username, color)
  } finally {
    savingColor[username] = false
  }
}
</script>

<template>
  <div>
    <h1>Calendar Settings</h1>
    <p class="subtitle">Business hours, time-slot size, default view, and each employee's color on the company calendar.</p>

    <div v-if="admin.loading && !admin.settings" class="hint">Loading…</div>

    <template v-else-if="admin.settings">
      <div class="card section">
        <h2>Business hours</h2>
        <p class="hint">Controls the visible time range and line spacing in the week/day calendar views.</p>
        <form class="hours-form" @submit.prevent="saveHours">
          <label>
            Start
            <input v-model="hours.start" type="time" required />
          </label>
          <label>
            End
            <input v-model="hours.end" type="time" required />
          </label>
          <label>
            Time slot size
            <select v-model.number="hours.slotMinutes">
              <option :value="15">15 minutes</option>
              <option :value="30">30 minutes</option>
              <option :value="45">45 minutes</option>
              <option :value="60">60 minutes</option>
            </select>
          </label>
          <label>
            Default view
            <select v-model="hours.defaultView">
              <option value="month">Month</option>
              <option value="week">Week</option>
              <option value="day">Day</option>
            </select>
          </label>
          <button type="submit" :disabled="savingHours">{{ savingHours ? 'Saving…' : 'Save' }}</button>
        </form>
      </div>

      <div class="card section">
        <h2>Member colors</h2>
        <p class="hint">Each employee's events on the shared company calendar show in their own color.</p>
        <ul class="member-list">
          <li v-for="(color, username) in admin.settings.member_colors" :key="username">
            <span class="username">{{ username }}</span>
            <input
              type="color"
              :value="color"
              :disabled="savingColor[username]"
              @change="setColor(username, ($event.target as HTMLInputElement).value)"
            />
          </li>
        </ul>
        <p v-if="Object.keys(admin.settings.member_colors).length === 0" class="hint">
          No employees yet — add some from the Users panel first.
        </p>
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
  margin: 0 0 1.5rem;
}
.hint {
  font-size: 0.85rem;
  color: var(--text-dim);
}
.section {
  padding: 1.1rem 1.2rem;
  max-width: 32rem;
  margin-bottom: 1.25rem;
}
.section h2 {
  font-size: 1rem;
  margin: 0 0 0.3rem;
}
.section > .hint {
  margin: 0 0 0.9rem;
}
.hours-form {
  display: flex;
  align-items: flex-end;
  gap: 0.85rem;
  flex-wrap: wrap;
}
.hours-form label {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  font-size: 0.85rem;
  font-weight: 500;
}
.member-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.member-list li {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  padding: 0.4rem 0;
  border-bottom: 1px solid var(--border);
}
.member-list li:last-child {
  border-bottom: none;
}
.username {
  font-size: 0.9rem;
  font-weight: 500;
}
input[type='color'] {
  width: 2.2rem;
  height: 1.8rem;
  padding: 0;
  border: 1px solid var(--border);
  border-radius: 6px;
  cursor: pointer;
}
</style>
