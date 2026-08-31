<script setup lang="ts">
import { onMounted, reactive } from 'vue'
import { useModulesStore } from '../stores/modules'
import type { Manifest } from '../api/types'

const modules = useModulesStore()
const openForms = reactive<Record<string, boolean>>({})
const formValues = reactive<Record<string, Record<string, string>>>({})
const busy = reactive<Record<string, boolean>>({})

onMounted(() => modules.fetchAll())

function statusFor(id: string) {
  return modules.statuses[id]?.status ?? 'not_installed'
}

// Nginx's fixed address on the internal VPN gateway's advertised route —
// see backend/internal/proxy/nginx.go's internalGatewayIP. Only means
// anything to someone already connected to the platform's VPN.
const INTERNAL_GATEWAY_IP = '172.28.0.2'

function hasPrimaryRoute(m: Manifest) {
  return m.routes?.some((r) => !r.name) ?? false
}

function isPrivate(id: string) {
  return modules.statuses[id]?.visibility === 'private'
}

async function toggleVisibility(id: string) {
  busy[id] = true
  try {
    await modules.setVisibility(id, isPrivate(id) ? 'public' : 'private')
  } finally {
    busy[id] = false
  }
}

function visibleFields(m: Manifest) {
  return m.config_schema.filter((f) => !f.hidden)
}

function linkUrl(hostname: string) {
  const port = window.location.port ? `:${window.location.port}` : ''
  return `${window.location.protocol}//${hostname}${port}`
}

function openInstallForm(m: Manifest) {
  formValues[m.id] = Object.fromEntries(m.config_schema.map((f) => [f.key, f.default]))
  openForms[m.id] = true
}

function generateSecret(moduleId: string, key: string) {
  const bytes = new Uint8Array(32)
  crypto.getRandomValues(bytes)
  formValues[moduleId][key] = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
}

async function submitInstall(id: string) {
  busy[id] = true
  try {
    await modules.install(id, formValues[id] ?? {})
    openForms[id] = false
  } finally {
    busy[id] = false
  }
}

async function run(id: string, action: 'enable' | 'disable' | 'uninstall') {
  busy[id] = true
  try {
    if (action === 'enable') await modules.enable(id)
    if (action === 'disable') await modules.disable(id)
    if (action === 'uninstall') await modules.uninstall(id)
  } finally {
    busy[id] = false
  }
}
</script>

<template>
  <div>
    <h1>Module Store</h1>
    <p class="subtitle">Install, configure, and manage the services running on this deployment.</p>
    <div class="grid">
      <div v-for="m in modules.catalog" :key="m.id" class="card">
        <h2>{{ m.name }}</h2>
        <p>{{ m.description }}</p>
        <p class="category">{{ m.category }}</p>

        <div v-if="statusFor(m.id) === 'installing'" class="installing">
          <span class="pill pill-neutral">installing…</span>
          <p class="hint">This can take a few minutes the first time (pulling images) — feel free to leave this page, it keeps running.</p>
        </div>

        <div v-else-if="statusFor(m.id) === 'not_installed' || statusFor(m.id) === 'error'">
          <p v-if="statusFor(m.id) === 'error'" class="error-message">
            Install failed: {{ modules.statuses[m.id]?.error_message }}
          </p>
          <button :class="{ secondary: !m.available }" :disabled="!m.available" @click="openInstallForm(m)">
            {{ !m.available ? 'Coming soon' : statusFor(m.id) === 'error' ? 'Retry install' : 'Install' }}
          </button>

          <form v-if="openForms[m.id]" @submit.prevent="submitInstall(m.id)" class="config-form">
            <label v-for="f in visibleFields(m)" :key="f.key">
              {{ f.label }}
              <div v-if="f.type === 'secret'" class="secret-field">
                <input
                  v-model="formValues[m.id][f.key]"
                  type="text"
                  placeholder="Leave blank to auto-generate"
                />
                <button type="button" @click="generateSecret(m.id, f.key)">Generate</button>
              </div>
              <input v-else v-model="formValues[m.id][f.key]" type="text" />
            </label>
            <button type="submit" :disabled="busy[m.id]">{{ busy[m.id] ? 'Starting…' : 'Confirm install' }}</button>
          </form>
        </div>

        <div v-else class="actions">
          <span class="pill" :class="statusFor(m.id) === 'running' ? 'pill-good' : 'pill-warn'">{{ statusFor(m.id) }}</span>
          <button v-if="statusFor(m.id) === 'stopped'" :disabled="busy[m.id]" @click="run(m.id, 'enable')">Enable</button>
          <button v-if="statusFor(m.id) === 'running'" class="secondary" :disabled="busy[m.id]" @click="run(m.id, 'disable')">Disable</button>
          <button class="secondary" :disabled="busy[m.id]" @click="run(m.id, 'uninstall')">Uninstall</button>

          <ul v-if="statusFor(m.id) === 'running' && (modules.links[m.id]?.length || m.internal_panel)" class="links">
            <li v-if="m.internal_panel">
              <router-link :to="m.internal_panel">Manage</router-link>
            </li>
            <li v-for="l in modules.links[m.id]" :key="l.name">
              <a :href="linkUrl(l.hostname)" target="_blank" rel="noopener">{{ l.name || 'Open' }}</a>
            </li>
          </ul>

          <div v-if="statusFor(m.id) === 'running' && hasPrimaryRoute(m)" class="visibility">
            <span class="pill" :class="isPrivate(m.id) ? 'pill-neutral' : 'pill-warn'">
              {{ isPrivate(m.id) ? 'Private (VPN only)' : 'Public (on the internet)' }}
            </span>
            <button class="secondary" :disabled="busy[m.id]" @click="toggleVisibility(m.id)">
              {{ isPrivate(m.id) ? 'Make public' : 'Make private' }}
            </button>
            <p v-if="isPrivate(m.id) && modules.statuses[m.id]?.private_port" class="hint">
              Reachable once connected to the VPN at
              <code>{{ INTERNAL_GATEWAY_IP }}:{{ modules.statuses[m.id]?.private_port }}</code>
            </p>
          </div>
        </div>
      </div>
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
  margin: 0 0 1.75rem;
}
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(270px, 1fr));
  gap: 1rem;
}
.card {
  padding: 1.1rem 1.2rem;
}
.card h2 {
  font-size: 1.05rem;
  margin-bottom: 0.35rem;
}
.card p {
  color: var(--text-dim);
  font-size: 0.88rem;
  line-height: 1.55;
  margin: 0 0 0.5rem;
}
.category {
  text-transform: uppercase;
  letter-spacing: 0.04em;
  font-family: var(--font-ui);
  font-weight: 700;
  font-size: 0.68rem;
  color: var(--text-faint);
  margin-bottom: 0.75rem;
}
.config-form {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  margin-top: 0.75rem;
}
.config-form label {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  font-size: 0.85rem;
  font-weight: 500;
}
.actions {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex-wrap: wrap;
}
.installing .hint {
  font-size: 0.85rem;
  color: var(--text-dim);
  margin-top: 0.5rem;
}
.error-message {
  color: var(--danger-text);
  font-size: 0.85rem;
  margin-bottom: 0.6rem;
}
.secret-field {
  display: flex;
  gap: 0.4rem;
}
.secret-field input {
  flex: 1;
  font-family: var(--font-mono);
}
.links {
  list-style: none;
  display: flex;
  gap: 0.75rem;
  margin: 0;
  padding: 0;
  width: 100%;
  font-size: 0.85rem;
  font-weight: 600;
}
.visibility {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex-wrap: wrap;
  margin-top: 0.5rem;
  padding-top: 0.65rem;
  border-top: 1px solid var(--border);
}
.visibility .hint {
  width: 100%;
  font-size: 0.82rem;
  color: var(--text-dim);
  margin: 0;
}
</style>
