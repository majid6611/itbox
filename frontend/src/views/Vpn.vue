<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useVpnStore } from '../stores/vpn'

const vpn = useVpnStore()
const busy = reactive<Record<string, boolean>>({})
const revealed = ref<{ username: string; setupKey: string } | null>(null)

onMounted(() => {
  vpn.fetchAll()
})

async function enable(username: string) {
  busy[username] = true
  try {
    const setupKey = await vpn.enable(username)
    revealed.value = { username, setupKey }
  } finally {
    busy[username] = false
  }
}

async function disable(username: string) {
  if (!confirm(`Remove VPN access for ${username}? Their setup key will stop working.`)) return
  busy[username] = true
  try {
    await vpn.disable(username)
    if (revealed.value?.username === username) revealed.value = null
  } finally {
    busy[username] = false
  }
}
</script>

<template>
  <div>
    <h1>VPN</h1>

    <div v-if="revealed" class="revealed">
      VPN setup key for <strong>{{ revealed.username }}</strong>: <code>{{ revealed.setupKey }}</code>
      <a class="button" :href="`/api/vpn/users/${revealed.username}/download`">Download setup file</a>
      <button type="button" @click="revealed = null">Dismiss</button>
    </div>

    <div v-if="!vpn.loading && !vpn.available" class="notice">
      The VPN module and Identity module both need to be installed and
      running before you can give people VPN access. Install them from the
      <router-link to="/modules">Module Store</router-link>.
    </div>

    <template v-else>
      <div v-if="!vpn.loading && !vpn.domainConfigured" class="notice warning">
        Set a real domain name in <router-link to="/settings">Settings</router-link>
        before giving anyone VPN access. Right now it's still "localhost," and a
        setup file built with that would only ever work on this computer — not for
        the person you send it to.
      </div>

      <p class="hint">
        Turn VPN access on for a user, then download their setup file — it has a
        one-time code they paste into the VPN app. No username or password needed.
      </p>

      <table class="users-table">
        <thead>
          <tr>
            <th>Username</th>
            <th>Name</th>
            <th>VPN access</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="u in vpn.users" :key="u.username">
            <td>{{ u.username }}</td>
            <td>{{ u.name }}</td>
            <td>
              <span :class="['badge', u.has_access ? 'running' : 'stopped']">{{ u.has_access ? 'enabled' : 'off' }}</span>
            </td>
            <td class="row-actions">
              <a
                v-if="u.has_access && vpn.domainConfigured"
                class="button"
                :href="`/api/vpn/users/${u.username}/download`"
              >Download setup file</a>
              <span v-else-if="u.has_access" class="button disabled" title="Set a domain in Settings first">Download setup file</span>
              <button v-if="u.has_access" :disabled="busy[u.username]" @click="disable(u.username)">Remove access</button>
              <button v-else :disabled="busy[u.username] || !vpn.domainConfigured" @click="enable(u.username)">Give VPN access</button>
            </td>
          </tr>
        </tbody>
      </table>
    </template>
  </div>
</template>

<style scoped>
.notice {
  border: 1px solid #333;
  border-radius: 8px;
  padding: 1rem;
  max-width: 32rem;
  margin-bottom: 1rem;
}
.notice.warning {
  border-color: #b08800;
}
.revealed {
  border: 1px solid #1a7f37;
  border-radius: 8px;
  padding: 0.75rem 1rem;
  margin-bottom: 1rem;
  display: flex;
  align-items: center;
  gap: 0.75rem;
  flex-wrap: wrap;
}
.revealed code {
  font-family: monospace;
  background: rgba(255, 255, 255, 0.08);
  padding: 0.1rem 0.4rem;
  border-radius: 4px;
}
.hint {
  font-size: 0.9rem;
  opacity: 0.8;
  max-width: 40rem;
  margin-bottom: 1.5rem;
}
.users-table {
  border-collapse: collapse;
  width: 100%;
}
.users-table th,
.users-table td {
  text-align: left;
  padding: 0.5rem;
  border-bottom: 1px solid #333;
}
.row-actions {
  display: flex;
  gap: 0.4rem;
  align-items: center;
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
  background: #555;
  color: white;
}
.button {
  display: inline-block;
  padding: 0.35rem 0.7rem;
  border: 1px solid #555;
  border-radius: 6px;
  text-decoration: none;
  color: inherit;
  font-size: 0.9rem;
}
.button.disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
