<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useModulesStore } from '../stores/modules'
import Icon from '../components/Icon.vue'

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
        <router-link v-for="m in modules.catalog" :key="m.id" to="/modules" class="row">
          <span class="row-name">{{ m.name }}</span>
          <span class="pill" :class="pillClass(statusFor(m.id))">{{ statusFor(m.id).replace('_', ' ') }}</span>
        </router-link>
      </div>
      <p v-if="modules.catalog.length === 0" class="hint">No modules in the catalog yet.</p>
      <router-link to="/modules" class="manage-link"><Icon name="modules" :size="15" /> Manage modules</router-link>
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
