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

onMounted(load)
</script>

<template>
  <div>
    <h1>Logs</h1>
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
.pager {
  display: flex;
  gap: 1rem;
  align-items: center;
}
</style>
