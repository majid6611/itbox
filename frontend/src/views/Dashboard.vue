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

// Last 7 days of disk / memory / load, one point per 5 minutes.
interface Sample { t: string; disk: number; mem: number; load: number }
const samples = ref<Sample[]>([])
const trends = computed(() => {
  const last = samples.value[samples.value.length - 1]
  if (samples.value.length < 2 || !last) return []
  return [
    { name: 'Disk', key: 'disk' as const, now: `${last.disk.toFixed(0)}%`, max: 100 },
    { name: 'Memory', key: 'mem' as const, now: `${last.mem.toFixed(0)}%`, max: 100 },
    { name: 'Load', key: 'load' as const, now: last.load.toFixed(2), max: Math.max(1, ...samples.value.map((x) => x.load)) },
  ].map((tr) => ({ ...tr, points: sparkPoints(tr.key, tr.max) }))
})
function sparkPoints(key: 'disk' | 'mem' | 'load', max: number) {
  const n = samples.value.length
  return samples.value.map((x, i) => `${((i / (n - 1)) * 100).toFixed(1)},${(30 - (x[key] / max) * 28).toFixed(1)}`).join(' ')
}

const query = ref('')
const moduleFilter = ref('all')
const loadError = ref('')
const refreshing = ref(false)
const healthUnavailable = ref(false)
const visibleModules = computed(() => modules.catalog.filter(m =>
  (moduleFilter.value === 'all' || (moduleFilter.value === 'installed' ? statusFor(m.id) !== 'not_installed' : ['stopped', 'error'].includes(statusFor(m.id)))) &&
  `${m.name} ${m.description}`.toLowerCase().includes(query.value.toLowerCase().trim()),
))
async function refresh() {
  refreshing.value = true
  loadError.value = ''
  const results = await Promise.allSettled([
    modules.fetchAll(), users.fetchAll(),
    api.get<{ checks: HealthCheck[] }>('/health/report'),
    api.get<{ samples: Sample[] }>('/health/history'),
  ])
  if (results[0].status === 'rejected') loadError.value = 'Could not load services. Try refreshing.'
  if (results[1].status === 'rejected') loadError.value += ' People data is currently unavailable.'
  const health = results[2]
  checks.value = health.status === 'fulfilled' ? health.value.checks ?? [] : null
  healthUnavailable.value = health.status === 'rejected'
  samples.value = results[3].status === 'fulfilled' ? results[3].value.samples ?? [] : []
  refreshing.value = false
}
onMounted(refresh)

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
    <div class="page-heading">
      <div><span class="eyebrow">YOUR WORKSPACE AT A GLANCE</span><h1>Overview</h1>
      <p class="subtitle">Your services, your people. Everything in one place.</p></div>
      <button class="secondary refresh" :disabled="refreshing" @click="refresh">{{ refreshing ? 'Refreshing…' : 'Refresh overview' }}</button>
    </div>
    <p v-if="loadError" class="notice" role="alert">{{ loadError }}</p>
    <p v-if="healthUnavailable" class="hint" role="status">Health report is currently unavailable.</p>

    <div v-if="checks" class="card health" :class="{ 'health-bad': problems.some((p) => p.status === 'bad') }">
      <p v-if="checks.length > 0 && problems.length === 0" class="health-ok"><span class="pill pill-good">Healthy</span><strong>All health checks passed</strong><span class="hint">No issues reported by your deployment.</span></p>
      <p v-else-if="checks.length === 0" class="hint">No health checks reported yet.</p>
      <template v-else>
        <p class="health-title">Needs attention</p>
        <p v-for="p in problems" :key="p.name" class="health-row">
          <span class="pill" :class="p.status === 'bad' ? 'pill-bad' : 'pill-warn'">{{ p.name }}</span>
          {{ p.detail }}
        </p>
      </template>
      <div v-if="trends.length" class="trends" aria-label="Last 7 days">
        <div v-for="tr in trends" :key="tr.name" class="trend">
          <span class="trend-name">{{ tr.name }} <strong>{{ tr.now }}</strong></span>
          <svg viewBox="0 0 100 32" preserveAspectRatio="none" role="img" :aria-label="`${tr.name}, last 7 days`"><polyline :points="tr.points" fill="none" stroke="currentColor" stroke-width="1.5" vector-effect="non-scaling-stroke" /></svg>
        </div>
        <span class="hint trend-note">Last 7 days</span>
      </div>
    </div>

    <p v-if="modules.loading" class="hint">Loading…</p>
    <template v-else>
      <div class="stats">
        <div class="stat card">
          <span class="stat-number">{{ installed.length }}</span>
          <span class="stat-label">Installed services</span>
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

      <section class="services-section">
      <div class="section-heading"><div><h2>Services</h2><p class="hint">Find and manage the tools your team relies on.</p></div><router-link to="/modules" class="section-link">Module Store <Icon name="arrow" :size="15" /></router-link></div>
      <div class="service-toolbar">
        <div class="filter-tabs" role="group" aria-label="Filter services"><button v-for="filter in [{ id: 'all', label: 'All services' }, { id: 'installed', label: 'Installed' }, { id: 'attention', label: 'Needs attention' }]" :key="filter.id" :aria-pressed="moduleFilter === filter.id" @click="moduleFilter = filter.id">{{ filter.label }}</button></div>
        <label class="service-search"><Icon name="search" :size="16" /><input v-model="query" type="search" aria-label="Search services" placeholder="Search services…" /></label>
      </div>
      <div class="card list" :aria-busy="refreshing">
        <router-link
          v-for="m in visibleModules"
          :key="m.id"
          :to="statusFor(m.id) === 'running' && m.internal_panel ? m.internal_panel : '/modules'"
          class="row"
        >
          <span class="service-icon"><Icon name="modules" :size="20" /></span><span class="service-copy"><span class="row-name">{{ m.name }}</span><span class="service-description">{{ m.description }}</span></span>
          <span class="pill" :class="pillClass(statusFor(m.id))">{{ statusFor(m.id).replaceAll('_', ' ') }}</span>
        </router-link>
      </div>
      <p v-if="!visibleModules.length" class="empty-state">{{ query || moduleFilter !== 'all' ? 'No services match your filters.' : 'Your workspace is ready for its first service.' }} <router-link v-if="!modules.catalog.length" to="/modules">Browse modules</router-link></p>
      </section>
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
.trends {
  display: flex;
  gap: 1.25rem;
  align-items: flex-end;
  flex-wrap: wrap;
  margin-top: 0.9rem;
  padding-top: 0.8rem;
  border-top: 1px solid var(--border, rgba(128, 128, 128, 0.25));
  color: var(--text-dim);
}
.trend { flex: 1 1 8rem; min-width: 7rem; }
.trend-name { display: block; font-size: 0.78rem; margin-bottom: 0.2rem; }
.trend-name strong { color: var(--text, inherit); margin-left: 0.3rem; }
.trend svg { width: 100%; height: 32px; display: block; }
.trend-note { font-size: 0.72rem; }
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

<style scoped>
.eyebrow { font-size: .65rem; font-weight: 700; letter-spacing: .14em; color: var(--accent); }
.page-heading { display: flex; justify-content: space-between; align-items: center; gap: 1rem; margin-bottom: 1.8rem; }
h1 { font-size: 2rem; letter-spacing: -.045em; margin-top: .35rem; }
.page-heading .subtitle { margin: .4rem 0 0; }
.refresh { white-space: nowrap; }
.notice { background: var(--danger-bg); color: var(--danger-text); padding: 1rem; border-radius: 10px; }
.health { background: var(--surface); padding: 1rem 1.25rem; }
.health-ok { display: flex; align-items: center; gap: .9rem; font-size: .85rem; flex-wrap: wrap; }
.health-ok .hint { margin-left: auto; font-size: .8rem; }
.stats { grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 1rem; margin: 1.5rem 0 2.25rem; }
.stat { flex-direction: column-reverse; padding: 1.4rem 1.5rem; gap: .7rem; }
.stat-number { font-size: 2.2rem; letter-spacing: -.04em; }
.stat-label { font-size: .8rem; }
.section-heading { display: flex; justify-content: space-between; align-items: center; gap: 1rem; margin-bottom: 1.25rem; }
.section-heading h2 { font-size: 1.1rem; margin: 0; }
.section-heading .hint { margin: .25rem 0 0; font-size: .8rem; }
.section-link { display: inline-flex; gap: .5rem; align-items: center; font-size: .8rem; font-weight: 600; white-space: nowrap; }
.service-toolbar { display: flex; justify-content: space-between; gap: 1rem; margin-bottom: 1rem; flex-wrap: wrap; }
.filter-tabs { display: flex; background: var(--surface-hover); padding: .25rem; border-radius: 9px; gap: .15rem; }
.filter-tabs button { background: transparent; color: var(--text-dim); font-size: .75rem; padding: .45rem .75rem; }
.filter-tabs button[aria-pressed='true'] { background: var(--surface); color: var(--text); box-shadow: var(--shadow-card); }
.service-search { display: flex; align-items: center; gap: .5rem; background: var(--surface); border: 1px solid var(--border-strong); border-radius: 8px; padding: 0 .75rem; color: var(--text-dim); }
.service-search input { border: 0; background: transparent; width: 190px; font-size: .8rem; }
.list { max-width: none; padding: .35rem 1rem; }
.row { padding: 1rem .25rem; border-radius: 0; gap: .9rem; }
.row + .row { border-top: 1px solid var(--border); }
.service-icon { display: flex; padding: .7rem; border-radius: 10px; color: var(--accent); background: var(--accent-soft); }
.service-copy { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: .2rem; }
.row-name { font-weight: 650; }
.service-description { font-size: .78rem; color: var(--text-dim); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.empty-state { text-align: center; color: var(--text-dim); padding: 2rem; font-size: .9rem; }
.people { max-width: none; margin-top: 2rem; overflow-x: auto; padding: 1.25rem; }
.people-head { margin-bottom: .75rem; }
.people-table td { padding: .85rem .5rem .85rem 0; }
@media (max-width: 700px) { .page-heading { align-items: flex-start; flex-direction: column; } .stats { gap: .5rem; } .stat { padding: 1rem .75rem; } .stat-number { font-size: 1.8rem; } .stat-label { font-size: .7rem; } .health-ok .hint { margin-left: 0; } .section-heading { align-items: flex-start; } .section-heading .hint { max-width: 200px; } .service-search { width: 100%; } .service-search input { width: 100%; } .filter-tabs { width: 100%; } .filter-tabs button { flex: 1; padding: .45rem; } .people-head { flex-wrap: wrap; } }
</style>
