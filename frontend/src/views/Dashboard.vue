<script setup lang="ts">
import { onMounted } from 'vue'
import { useModulesStore } from '../stores/modules'

const modules = useModulesStore()

onMounted(() => modules.fetchAll())

function statusFor(id: string) {
  return modules.statuses[id]?.status ?? 'not_installed'
}
</script>

<template>
  <div>
    <h1>Dashboard</h1>
    <p v-if="modules.loading">Loading…</p>
    <table v-else>
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
            <span :class="['badge', statusFor(m.id)]">{{ statusFor(m.id) }}</span>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-if="!modules.loading && modules.catalog.length === 0">No modules in the catalog yet.</p>
  </div>
</template>

<style scoped>
table {
  border-collapse: collapse;
  width: 100%;
  max-width: 640px;
}
th,
td {
  text-align: left;
  padding: 0.5rem;
  border-bottom: 1px solid #333;
}
.badge {
  padding: 0.15rem 0.5rem;
  border-radius: 4px;
  font-size: 0.85rem;
}
.badge.running {
  background: #1a7f37;
  color: white;
}
.badge.stopped {
  background: #b08800;
  color: white;
}
.badge.not_installed {
  background: #555;
  color: white;
}
</style>
