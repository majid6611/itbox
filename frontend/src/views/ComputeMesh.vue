<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useComputeMeshStore } from '../stores/computeMesh'

const mesh = useComputeMeshStore()
const busy = reactive<Record<number, boolean>>({})
const showAddForm = ref(false)
const form = reactive({ name: '', host: '', amtUsername: 'admin', amtPassword: '' })
const addError = ref('')
const adding = ref(false)

onMounted(() => {
  mesh.fetchAll()
})

async function addDevice() {
  adding.value = true
  addError.value = ''
  try {
    await mesh.addDevice(form.name, form.host, form.amtUsername, form.amtPassword)
    form.name = ''
    form.host = ''
    form.amtPassword = ''
    showAddForm.value = false
  } catch {
    addError.value = 'Could not register this device — check the AMT address and credentials.'
  } finally {
    adding.value = false
  }
}

async function removeDevice(id: number, name: string) {
  if (!confirm(`Remove "${name}"? You'll need to register it again to control it.`)) return
  busy[id] = true
  try {
    await mesh.removeDevice(id)
  } finally {
    busy[id] = false
  }
}

async function power(id: number, action: 'on' | 'off' | 'cycle') {
  if (action === 'off' && !confirm('Power off this computer now?')) return
  if (action === 'cycle' && !confirm('Restart this computer now?')) return
  busy[id] = true
  try {
    await mesh.power(id, action)
  } finally {
    busy[id] = false
  }
}
</script>

<template>
  <div>
    <h1>Compute Mesh</h1>
    <p class="subtitle">Turn company computers on, off, or restart them remotely.</p>

    <div v-if="!mesh.loading && !mesh.available" class="notice">
      The Compute Mesh module needs to be installed and running first —
      install it from the <router-link to="/modules">Module Store</router-link>.
    </div>

    <template v-else>
      <p class="hint">
        Works even if the computer's operating system has crashed or hung, as
        long as it has Intel AMT/vPro set up (in its BIOS/MEBx) and is
        reachable on the network — power control happens at the hardware
        level, independent of the OS. Setting up AMT itself on a machine
        isn't done here, only registering one that already has it.
      </p>

      <button class="new-device-btn" @click="showAddForm = !showAddForm">+ Register a computer</button>
      <form v-if="showAddForm" class="add-form card" @submit.prevent="addDevice">
        <label>
          Name
          <input v-model="form.name" type="text" placeholder="Front desk PC" required />
        </label>
        <label>
          AMT address
          <input v-model="form.host" type="text" placeholder="192.168.1.50" required />
        </label>
        <label>
          AMT username
          <input v-model="form.amtUsername" type="text" required />
        </label>
        <label>
          AMT password
          <input v-model="form.amtPassword" type="password" required />
        </label>
        <div class="add-form-actions">
          <button type="submit" :disabled="adding">{{ adding ? 'Registering…' : 'Register' }}</button>
          <button type="button" class="secondary" @click="showAddForm = false">Cancel</button>
        </div>
        <p v-if="addError" class="error-message">{{ addError }}</p>
      </form>

      <table v-if="mesh.devices.length" class="devices-table">
        <thead>
          <tr>
            <th>Name</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="d in mesh.devices" :key="d.id">
            <td>{{ d.name }}</td>
            <td class="row-actions">
              <button class="secondary" :disabled="busy[d.id]" @click="power(d.id, 'on')">Power on</button>
              <button class="secondary" :disabled="busy[d.id]" @click="power(d.id, 'off')">Power off</button>
              <button class="secondary" :disabled="busy[d.id]" @click="power(d.id, 'cycle')">Restart</button>
              <button class="secondary" :disabled="busy[d.id]" @click="removeDevice(d.id, d.name)">Remove</button>
            </td>
          </tr>
        </tbody>
      </table>
      <p v-else class="hint">No computers registered yet.</p>
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
  font-size: 0.9rem;
  color: var(--text-dim);
  max-width: 42rem;
  margin-bottom: 1.5rem;
  line-height: 1.55;
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
.new-device-btn {
  margin-bottom: 0.75rem;
}
.add-form {
  display: flex;
  flex-wrap: wrap;
  gap: 0.85rem;
  align-items: flex-end;
  margin-bottom: 1.5rem;
  padding: 1.1rem;
}
.add-form label {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  font-size: 0.85rem;
  font-weight: 500;
}
.add-form-actions {
  display: flex;
  gap: 0.5rem;
}
.error-message {
  width: 100%;
  color: var(--danger-text);
  font-size: 0.85rem;
  margin: 0;
}
.devices-table {
  border-collapse: collapse;
  width: 100%;
}
.devices-table th {
  text-align: left;
  padding: 0.6rem 0.7rem;
  font-family: var(--font-ui);
  font-size: 0.72rem;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--text-faint);
  border-bottom: 1px solid var(--border);
}
.devices-table td {
  text-align: left;
  padding: 0.6rem 0.7rem;
  border-bottom: 1px solid var(--border);
}
.row-actions {
  display: flex;
  gap: 0.4rem;
  flex-wrap: wrap;
}
</style>
