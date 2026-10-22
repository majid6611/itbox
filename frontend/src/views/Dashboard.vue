<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api } from '../api/client'
import { useUsersStore } from '../stores/users'
import { timeAgo } from '../utils/time'
import { useModulesStore } from '../stores/modules'
import Icon from '../components/Icon.vue'

const modules = useModulesStore()
const users = useUsersStore()

// Most recent sign-in first; people who never signed in go last.
const people = computed(() =>
  [...users.users]
    .filter((u) => !u.disabled)
    .sort((a, b) => (b.last_login ?? '').localeCompare(a.last_login ?? '') || a.username.localeCompare(b.username)),
)
const signedInCount = computed(() => people.value.filter((u) => u.signed_in).length)

interface HealthCheck {
  name: string
  status: 'ok' | 'warn' | 'bad'
  detail: string
}
const checks = ref<HealthCheck[] | null>(null)
const problems = computed(() => (checks.value ?? []).filter((c) => c.status !== 'ok'))

onMounted(async () => {
  modules.fetchAll()
  users.fetchAll()
  try {
    checks.value = (await api.get<{ checks: HealthCheck[] }>('/health/report')).checks ?? []
  } catch {
    checks.value = null
  }
})

function statusFor(id: string) {
  return modules.statuses[id]?.status ?? 'not_installed'
}

function pillClass(status: string) {
  if (status === 'running') return 'pill-good'
  if (status === 'stopped' || status === 'installing') return 'pill-warn'
  if (status === 'error') return 'pill-bad'
  return 'pill-neutral'
}

// Only modules an admin has actually installed are worth summarizing —
// the rest of the catalog is "not yet part of this deployment," not a
// problem to flag alongside real running/stopped services.
const installed = computed(() => modules.catalog.filter((m) => statusFor(m.id) !== 'not_installed'))
const runningCount = computed(() => installed.value.filter((m) => statusFor(m.id) === 'running').length)
const attentionCount = computed(() => installed.value.filter((m) => ['stopped', 'error'].includes(statusFor(m.id))).length)
</script>

<template>
  <div>
    <h1>Dashboard</h1>
    <p class="subtitle">What's running on this deployment right now.</p>

    <div v-if="checks" class="card health" :class="{ 'health-bad': problems.some((p) => p.status === 'bad') }">
      <p v-if="problems.length === 0" class="health-ok"><span class="pill pill-good">Everything is OK</span></p>
      <template v-else>
        <p class="health-title">Needs attention</p>
        <p v-for="p in problems" :key="p.name" class="health-row">
          <span class="pill" :class="p.status === 'bad' ? 'pill-bad' : 'pill-warn'">{{ p.name }}</span>
          {{ p.detail }}
        </p>
      </template>
    </div>

    <p v-if="modules.loading" class="hint">Loading…</p>
    <template v-else>
      <div class="stats">
        <div class="stat card">
          <span class="stat-number">{{ installed.length }}</span>
          <span class="stat-label">Installed</span>
        </div>
        <div class="stat card">
          <span class="stat-number good">{{ runningCount }}</span>
          <span class="stat-label">Running</span>
        </div>
        <div class="stat card" :class="{ warn: attentionCount > 0 }">
          <span class="stat-number" :class="{ warn: attentionCount > 0 }">{{ attentionCount }}</span>
          <span class="stat-label">Needs attention</span>
        </div>
      </div>

      <div class="card list">
        <router-link
          v-for="m in modules.catalog"
          :key="m.id"
          :to="statusFor(m.id) === 'running' && m.internal_panel ? m.internal_panel : '/modules'"
          class="row"
        >
          <span class="row-name">{{ m.name }}</span>
          <span class="pill" :class="pillClass(statusFor(m.id))">{{ statusFor(m.id).replace('_', ' ') }}</span>
        </router-link>
      </div>
      <p v-if="modules.catalog.length === 0" class="hint">No modules in the catalog yet.</p>
      <div v-if="users.available && people.length" class="card people">
        <div class="people-head">
          <h2>People</h2>
          <span class="hint">{{ signedInCount }} signed in · <router-link to="/users">Manage users</router-link></span>
        </div>
        <table class="people-table">
          <thead>
            <tr><th>Person</th><th>Group</th><th>Last sign-in</th></tr>
          </thead>
          <tbody>
            <tr v-for="u in people" :key="u.username">
              <td>
                <strong>{{ u.name || u.username }}</strong> <span class="uname">{{ u.username }}</span>
                <span v-if="u.signed_in" class="pill pill-good">signed in</span>
              </td>
              <td>{{ u.group || '—' }}</td>
              <td :title="u.last_login ? new Date(u.last_login).toLocaleString() + (u.last_login_ip ? ' from ' + u.last_login_ip : '') : ''">
                {{ (u.last_login || !u.signed_in ? timeAgo(u.last_login) : 'earlier (before tracking began)') }}
              </td>
            </tr>
          </tbody>
        </table>
        <p class="hint small">
          Counts sign-ins to the employee portal. "Signed in" means an open session (they stay signed in for up to a
          week), not that someone is using it this minute.
        </p>
      </div>
      <router-link to="/modules" class="manage-link"><Icon name="modules" :size="15" /> Manage modules</router-link>
    </template>
  </div>
</template>

<style scoped>
.people {
  padding: 1rem 1.2rem;
  margin: 1.25rem 0;
  max-width: 44rem;
}
.people-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 1rem;
}
.people-head h2 {
  margin: 0 0 0.5rem;
  font-size: 1rem;
}
.people-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.9rem;
}
.people-table th {
  text-align: left;
  font-size: 0.72rem;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--text-faint);
  padding: 0.3rem 0.5rem 0.3rem 0;
}
.people-table td {
  padding: 0.4rem 0.5rem 0.4rem 0;
  border-top: 1px solid var(--border);
}
.uname {
  color: var(--text-faint);
  font-size: 0.82rem;
  margin: 0 0.4rem;
}
.hint.small {
  font-size: 0.78rem;
  margin: 0.6rem 0 0;
}
.health {
  padding: 0.9rem 1.1rem;
  margin-bottom: 1.25rem;
}
.health-title {
  font-weight: 600;
  margin: 0 0 0.5rem;
}
.health-row {
  margin: 0.3rem 0;
  font-size: 0.9rem;
  color: var(--text-dim);
}
.health-ok {
  margin: 0;
}
h1 {
  margin-bottom: 0.35rem;
}
.subtitle {
  color: var(--text-dim);
  font-size: 0.92rem;
  margin: 0 0 1.75rem;
}
.hint {
  color: var(--text-dim);
  font-size: 0.9rem;
}

.stats {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 10rem));
  gap: 0.85rem;
  margin-bottom: 1.75rem;
}
.stat {
  padding: 1rem 1.2rem;
  display: flex;
  flex-direction: column;
  gap: 0.15rem;
}
.stat.warn {
  border-color: var(--warning-text);
}
.stat-number {
  font-family: var(--font-ui);
  font-size: 1.9rem;
  font-weight: 800;
  line-height: 1.1;
}
.stat-number.good {
  color: var(--success-text);
}
.stat-number.warn {
  color: var(--warning-text);
}
.stat-label {
  font-size: 0.78rem;
  color: var(--text-dim);
  font-weight: 500;
}

.list {
  max-width: 34rem;
  padding: 0.4rem;
}
.row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.65rem 0.8rem;
  border-radius: 8px;
  color: var(--text);
}
.row:hover {
  background: var(--surface-hover);
}
.row-name {
  font-size: 0.9rem;
  font-weight: 500;
}
.manage-link {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  margin-top: 1rem;
  font-size: 0.88rem;
  font-weight: 600;
}
</style>
