<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { api } from '../api/client'

interface AuditEntry {
  id: number
  at: string
  actor: string
  actor_kind: string
  action: string
  target: string
  ip: string
  status: number
  detail: string
}

const PAGE = 100
const tab = ref<'activity' | 'system'>('activity')

interface SystemEvent {
  id: number
  at: string
  level: string
  source: string
  message: string
}
const events = ref<SystemEvent[]>([])
const sysTotal = ref(0)
const sysOffset = ref(0)
const sysSources = ref<string[]>([])
const sysLoading = ref(false)
const sysError = ref('')
const sysFilter = reactive({ level: '', source: '', search: '', from: '', to: '' })
const entries = ref<AuditEntry[]>([])
const total = ref(0)
const offset = ref(0)
const loading = ref(false)
const error = ref('')
const filter = reactive({ actor: '', action: '', from: '', to: '' })
const verifyResult = ref<{ ok: boolean; checked: number; broke_at?: number; missing?: boolean } | null>(null)
const verifying = ref(false)

function query(extra: Record<string, string | number> = {}) {
  const p = new URLSearchParams()
  for (const [k, v] of Object.entries<string | number>({ ...filter, ...extra })) if (v !== '' && v !== 0) p.set(k, String(v))
  const s = p.toString()
  return s ? `?${s}` : ''
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const res = await api.get<{ entries: AuditEntry[]; total: number }>(`/audit${query({ limit: PAGE, offset: offset.value })}`)
    entries.value = res.entries ?? []
    total.value = res.total
  } catch {
    error.value = "Couldn't load the activity log."
  } finally {
    loading.value = false
  }
}

function search() {
  offset.value = 0
  load()
}

function page(delta: number) {
  offset.value = Math.max(0, offset.value + delta * PAGE)
  load()
}

async function verify() {
  verifying.value = true
  try {
    verifyResult.value = await api.get('/audit/verify')
  } catch {
    error.value = "Couldn't check the log."
  } finally {
    verifying.value = false
  }
}

// A plain navigation: the browser sends the session cookie and saves the file.
function download() {
  window.location.href = `/api/audit/export${query()}`
}

function when(iso: string) {
  return new Date(iso).toLocaleString()
}

function ok(status: number) {
  return status === 0 || status < 400
}

function sysQuery(extra: Record<string, string | number> = {}) {
  const p = new URLSearchParams()
  for (const [k, v] of Object.entries<string | number>({ ...sysFilter, ...extra })) if (v !== '' && v !== 0) p.set(k, String(v))
  const q = p.toString()
  return q ? `?${q}` : ''
}

async function loadSystem() {
  sysLoading.value = true
  sysError.value = ''
  try {
    const res = await api.get<{ entries: SystemEvent[]; total: number; sources: string[] }>(
      `/system-log${sysQuery({ limit: PAGE, offset: sysOffset.value })}`,
    )
    events.value = res.entries ?? []
    sysTotal.value = res.total
    sysSources.value = res.sources ?? []
  } catch {
    sysError.value = "Couldn't load the system log."
  } finally {
    sysLoading.value = false
  }
}

function searchSystem() {
  sysOffset.value = 0
  loadSystem()
}

function pageSystem(delta: number) {
  sysOffset.value = Math.max(0, sysOffset.value + delta * PAGE)
  loadSystem()
}

function downloadSystem() {
  window.location.href = `/api/system-log/export${sysQuery()}`
}

function showSystem() {
  tab.value = 'system'
  loadSystem()
}

onMounted(load)
</script>

<template>
  <div>
    <h1>Logs</h1>
    <div class="tabs">
      <button :class="tab === 'activity' ? 'tab on' : 'tab'" @click="tab = 'activity'">Activity</button>
      <button :class="tab === 'system' ? 'tab on' : 'tab'" @click="showSystem">System</button>
    </div>

    <template v-if="tab === 'activity'">
    <p class="subtitle">
      Who did what. Entries can't be edited or deleted, and are kept for one year.
    </p>

    <form class="filters" @submit.prevent="search">
      <label>Who <input v-model="filter.actor" type="text" placeholder="name or email" /></label>
      <label>Action <input v-model="filter.action" type="text" placeholder="e.g. login, install" /></label>
      <label>From <input v-model="filter.from" type="date" /></label>
      <label>To <input v-model="filter.to" type="date" /></label>
      <button type="submit">Search</button>
      <button type="button" class="secondary" @click="download">Download CSV</button>
      <button type="button" class="secondary" :disabled="verifying" @click="verify">
        {{ verifying ? 'Checking…' : 'Check the log is intact' }}
      </button>
    </form>

    <p v-if="verifyResult && verifyResult.ok" class="result good">
      All {{ verifyResult.checked }} entries are intact — none were changed or removed.
    </p>
    <p v-else-if="verifyResult" class="result bad">
      <template v-if="verifyResult.missing">An entry before #{{ verifyResult.broke_at }} was removed.</template>
      <template v-else>Entry #{{ verifyResult.broke_at }} was changed after it was written.</template>
      The log has been tampered with.
    </p>
    <p v-if="error" class="result bad">{{ error }}</p>

    <p v-if="!loading && entries.length === 0 && !error" class="hint">Nothing matches.</p>
    <table v-else class="logs-table">
      <thead>
        <tr>
          <th>When</th>
          <th>Who</th>
          <th>Action</th>
          <th>Target</th>
          <th>From</th>
          <th>Result</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="e in entries" :key="e.id">
          <td class="nowrap">{{ when(e.at) }}</td>
          <td>
            {{ e.actor || '—' }}
            <span v-if="e.actor_kind" class="hint">{{ e.actor_kind }}</span>
          </td>
          <td>{{ e.action }}</td>
          <td>{{ e.target || '' }}</td>
          <td>{{ e.ip }}</td>
          <td><span class="pill" :class="ok(e.status) ? 'pill-good' : 'pill-bad'">{{ e.status || 'ok' }}</span></td>
        </tr>
      </tbody>
    </table>

    <div v-if="total > PAGE" class="pager">
      <button class="secondary" :disabled="offset === 0" @click="page(-1)">Newer</button>
      <span class="hint">{{ offset + 1 }}–{{ Math.min(offset + PAGE, total) }} of {{ total }}</span>
      <button class="secondary" :disabled="offset + PAGE >= total" @click="page(1)">Older</button>
    </div>
    </template>

    <template v-else>
      <p class="subtitle">
        What the platform and its modules did — crashes, recoveries, updates, backups. Kept for 90 days.
      </p>
      <form class="filters" @submit.prevent="searchSystem">
        <label>
          Show
          <select v-model="sysFilter.level">
            <option value="">Everything</option>
            <option value="warn">Warnings and errors</option>
            <option value="error">Errors only</option>
          </select>
        </label>
        <label>
          Source
          <select v-model="sysFilter.source">
            <option value="">Everywhere</option>
            <option v-for="src in sysSources" :key="src" :value="src">{{ src }}</option>
          </select>
        </label>
        <label>Contains <input v-model="sysFilter.search" type="text" /></label>
        <label>From date <input v-model="sysFilter.from" type="date" /></label>
        <label>To date <input v-model="sysFilter.to" type="date" /></label>
        <button type="submit">Search</button>
        <button type="button" class="secondary" @click="downloadSystem">Download CSV</button>
      </form>
      <p v-if="sysError" class="result bad">{{ sysError }}</p>
      <p v-if="!sysLoading && events.length === 0 && !sysError" class="hint">Nothing matches.</p>
      <table v-else class="logs-table">
        <thead>
          <tr>
            <th>When</th>
            <th>Level</th>
            <th>From</th>
            <th>What happened</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="e in events" :key="e.id">
            <td class="nowrap">{{ when(e.at) }}</td>
            <td>
              <span class="pill" :class="e.level === 'error' ? 'pill-bad' : e.level === 'warn' ? 'pill-warn' : 'pill-neutral'">{{ e.level }}</span>
            </td>
            <td>{{ e.source }}</td>
            <td class="message">{{ e.message }}</td>
          </tr>
        </tbody>
      </table>
      <div v-if="sysTotal > PAGE" class="pager">
        <button class="secondary" :disabled="sysOffset === 0" @click="pageSystem(-1)">Newer</button>
        <span class="hint">{{ sysOffset + 1 }}–{{ Math.min(sysOffset + PAGE, sysTotal) }} of {{ sysTotal }}</span>
        <button class="secondary" :disabled="sysOffset + PAGE >= sysTotal" @click="pageSystem(1)">Older</button>
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
.filters {
  display: flex;
  gap: 0.75rem;
  align-items: flex-end;
  flex-wrap: wrap;
  margin-bottom: 1rem;
}
.filters label {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  font-size: 0.8rem;
  color: var(--text-dim);
}
.result {
  padding: 0.6rem 0.9rem;
  border-radius: 10px;
  font-size: 0.9rem;
  margin: 0 0 1rem;
}
.result.good {
  color: var(--success-text);
  background: var(--success-bg);
}
.result.bad {
  color: var(--danger-text, #b42318);
  border: 1px solid currentColor;
}
.logs-table {
  width: 100%;
  border-collapse: collapse;
  margin-bottom: 1rem;
}
.logs-table th {
  text-align: left;
  padding: 0.6rem 0.7rem;
  font-family: var(--font-ui);
  font-size: 0.72rem;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--text-faint);
  border-bottom: 1px solid var(--border);
}
.logs-table td {
  padding: 0.5rem 0.7rem;
  border-bottom: 1px solid var(--border);
  font-size: 0.9rem;
}
.nowrap {
  white-space: nowrap;
}
.hint {
  font-size: 0.78rem;
  color: var(--text-faint);
}
.tabs {
  display: flex;
  gap: 0.25rem;
  margin: 0.5rem 0 1rem;
  border-bottom: 1px solid var(--border);
}
.tab {
  background: none;
  border: none;
  border-bottom: 2px solid transparent;
  border-radius: 0;
  padding: 0.5rem 1rem;
  color: var(--text-dim);
}
.tab.on {
  color: var(--text);
  border-bottom-color: var(--text);
}
.message {
  word-break: break-word;
}
.pager {
  display: flex;
  gap: 1rem;
  align-items: center;
}
</style>
