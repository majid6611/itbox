<script setup lang="ts">
import { onMounted } from 'vue'
import { useModulesStore } from '../stores/modules'

const modules = useModulesStore()

onMounted(() => modules.fetchAll())

function statusFor(id: string) {
  return modules.statuses[id]?.status ?? 'not_installed'
}

function pillClass(status: string) {
  if (status === 'running') return 'pill-good'
  if (status === 'stopped' || status === 'installing') return 'pill-warn'
  if (status === 'error') return 'pill-bad'
  return 'pill-neutral'
}
</script>

<template>
  <div>
    <h1>Dashboard</h1>
    <p v-if="modules.loading" class="hint">Loading…</p>
    <table v-else class="card">
      <thead>
        <tr>
          <th>Module</th>
          <th>Status</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="m in modules.catalog" :key="m.id">
          <td>{{ m.name }}</td>
          <td>
            <span class="pill" :class="pillClass(statusFor(m.id))">{{ statusFor(m.id) }}</span>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-if="!modules.loading && modules.catalog.length === 0" class="hint">No modules in the catalog yet.</p>
  </div>
</template>

<style scoped>
h1 {
  margin-bottom: 1.5rem;
}
table {
  border-collapse: collapse;
  width: 100%;
  max-width: 36rem;
  overflow: hidden;
}
th,
td {
  text-align: left;
  padding: 0.65rem 1rem;
}
th {
  font-family: var(--font-ui);
  font-size: 0.72rem;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--text-faint);
  border-bottom: 1px solid var(--border);
}
tbody tr:not(:last-child) td {
  border-bottom: 1px solid var(--border);
}
.hint {
  color: var(--text-dim);
  font-size: 0.9rem;
}
</style>
