<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useVpnStore } from '../stores/vpn'
import { ApiError } from '../api/client'
import type { VpnDevice, VpnUser } from '../api/types'

const vpn = useVpnStore()
const busy = reactive<Record<string, boolean>>({})
const revealed = ref<{ username: string; setupKey: string; email: string; password: string } | null>(null)
const twoFactorError = ref('')

const officeEnabled = ref(false)
const officeNetwork = ref('')
const officeSaving = ref(false)
const officeSaved = ref(false)
const officeError = ref('')

onMounted(async () => {
  vpn.fetchAll()
  vpn.fetchTwoFactor().catch(() => {})
  await vpn.fetchOfficeNetwork().catch(() => {})
  officeEnabled.value = vpn.office.enabled
  officeNetwork.value = vpn.office.network || vpn.office.detected
})

async function saveOffice() {
  officeSaving.value = true
  officeSaved.value = false
  officeError.value = ''
  try {
    await vpn.saveOfficeNetwork(officeEnabled.value, officeNetwork.value)
    officeSaved.value = true
  } catch (e) {
    let msg = 'Could not save'
    if (e instanceof ApiError) {
      try {
        msg = JSON.parse(e.message).detail ?? msg
      } catch {
        msg = e.message || msg
      }
    }
    officeError.value = msg
  } finally {
    officeSaving.value = false
  }
}

async function enable(username: string) {
  busy[username] = true
  try {
    const res = await vpn.enable(username)
    revealed.value = { username, setupKey: res.setup_key, email: res.login_email, password: res.login_password }
  } finally {
    busy[username] = false
  }
}

const STALE_DAYS = 30

function owner(d: VpnDevice) {
  const g = d.groups?.find((g) => g.name.startsWith('user-'))
  return g ? g.name.slice(5) : '—'
}

function isApproved(d: VpnDevice) {
  return !!d.groups?.some((g) => g.name === 'vpn-approved')
}

function isStale(d: VpnDevice) {
  return Date.now() - new Date(d.last_seen).getTime() > STALE_DAYS * 86400000
}

function keyStatus(u: VpnUser) {
  if (!u.key_expires_at) return ''
  return new Date(u.key_expires_at).getTime() > Date.now()
    ? `key valid until ${new Date(u.key_expires_at).toLocaleString()}`
    : 'key expired or used'
}

async function approveDevice(d: VpnDevice) {
  await vpn.approveDevice(d.id)
}

async function removeDevice(d: VpnDevice) {
  if (!confirm(`Remove ${d.name} from the VPN? It is cut off right away.`)) return
  await vpn.removeDevice(d.id)
}

async function toggleTwoFactor(e: Event) {
  const on = (e.target as HTMLInputElement).checked
  twoFactorError.value = ''
  try {
    await vpn.saveTwoFactor(on)
  } catch {
    twoFactorError.value = 'Could not change this setting'
    ;(e.target as HTMLInputElement).checked = !on
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
      <template v-if="revealed.email">
        VPN login for <strong>{{ revealed.username }}</strong>: <code>{{ revealed.email }}</code> /
        <code>{{ revealed.password }}</code> (shown once — it is also in the setup file)
      </template>
      <template v-else>
        VPN setup key for <strong>{{ revealed.username }}</strong>: <code>{{ revealed.setupKey }}</code>
      </template>
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
        The code works for one device and expires after 24 hours; use "New key" to
        issue another (for a second device, or if it ran out).
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
              <span class="pill" :class="u.has_access ? 'pill-good' : 'pill-neutral'">{{ u.has_access ? 'enabled' : 'off' }}</span>
              <span v-if="u.has_access && u.login" class="hint-inline"> signs in with 2FA</span>
              <span v-else-if="u.has_access" class="hint-inline"> {{ keyStatus(u) }}</span>
            </td>
            <td class="row-actions">
              <a
                v-if="u.has_access && vpn.domainConfigured"
                class="button"
                :href="`/api/vpn/users/${u.username}/download`"
              >Download setup file</a>
              <span v-else-if="u.has_access" class="button disabled" title="Set a domain in Settings first">Download setup file</span>
              <button v-if="u.has_access" class="secondary" :disabled="busy[u.username] || !vpn.domainConfigured" @click="enable(u.username)">{{ vpn.twoFactor ? 'Reset login' : 'New key' }}</button>
              <button v-if="u.has_access" class="secondary" :disabled="busy[u.username]" @click="disable(u.username)">Remove access</button>
              <button v-else :disabled="busy[u.username] || !vpn.domainConfigured" @click="enable(u.username)">Give VPN access</button>
            </td>
          </tr>
        </tbody>
      </table>

      <h2>Two-factor login (2FA)</h2>
      <p class="hint">
        Off: people get a setup key. On: people you give VPN access to afterwards get an email and
        password instead, and every sign-in also asks for the 6-digit code from an authenticator app
        (Google Authenticator or similar). Their phone is linked on first sign-in. Anyone who already
        has access keeps what they have until you use "Reset login" for them. If someone loses their
        phone, "Reset login" gives them a fresh start.
      </p>
      <label class="check">
        <input type="checkbox" :checked="vpn.twoFactor" @change="toggleTwoFactor" />
        Require password and authenticator code
      </label>
      <p v-if="twoFactorError" class="error">{{ twoFactorError }}</p>

      <h2>Office network</h2>
      <p class="hint">
        Let people on the VPN reach the computers at the office — file shares, printers, remote
        desktop and office phones — as if they were plugged in there. Off means they can only reach
        this platform's own services.
      </p>
      <form class="office-form" @submit.prevent="saveOffice">
        <label class="check">
          <input v-model="officeEnabled" type="checkbox" :disabled="!vpn.office.available" />
          VPN users can reach the office network
        </label>
        <label v-if="officeEnabled">
          Office network
          <input v-model.trim="officeNetwork" type="text" placeholder="192.168.1.0/24" />
          <span v-if="vpn.office.detected" class="hint-inline">
            This server is on <code>{{ vpn.office.detected }}</code>
          </span>
        </label>
        <div class="save-row">
          <button :disabled="officeSaving || !vpn.office.available">{{ officeSaving ? 'Saving…' : 'Save' }}</button>
          <span v-if="officeSaved" class="saved">Saved — VPN devices pick this up within a minute.</span>
          <span v-if="officeError" class="error">{{ officeError }}</span>
        </div>
      </form>

      <h2>Connected devices</h2>
      <p class="hint">
        Every device enrolled, straight from the VPN server. A new device can't reach
        anything until you approve it here, so approve only the ones you expect. Remove any you don't
        recognise or that nobody uses any more — it is cut off right away. Devices
        not seen for {{ STALE_DAYS }}+ days are marked.
      </p>
      <table class="users-table" v-if="vpn.devices.length">
        <thead>
          <tr>
            <th>Device</th>
            <th>Owner</th>
            <th>IP</th>
            <th>Status</th>
            <th>Last seen</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="d in vpn.devices" :key="d.id">
            <td>{{ d.name }} <span class="hint-inline">({{ d.os }})</span></td>
            <td>{{ owner(d) }}</td>
            <td><code>{{ d.ip }}</code></td>
            <td>
              <span v-if="!isApproved(d)" class="pill pill-warn">waiting for approval</span>
              <span v-else class="pill" :class="d.connected ? 'pill-good' : 'pill-neutral'">{{ d.connected ? 'connected' : 'offline' }}</span>
            </td>
            <td>
              {{ new Date(d.last_seen).toLocaleString() }}
              <span v-if="isStale(d)" class="pill pill-warn">unused {{ STALE_DAYS }}+ days</span>
            </td>
            <td class="row-actions">
              <button v-if="!isApproved(d)" @click="approveDevice(d)">Approve</button>
              <button class="secondary" @click="removeDevice(d)">Remove</button>
            </td>
          </tr>
        </tbody>
      </table>
      <p v-else class="hint">No devices have enrolled yet.</p>
    </template>
  </div>
</template>

<style scoped>
.office-form {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  max-width: 28rem;
}
.office-form label {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  font-size: 0.88rem;
  font-weight: 600;
}
.office-form label.check {
  flex-direction: row;
  align-items: center;
  gap: 0.5rem;
}
.save-row {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}
.saved {
  color: var(--success-text);
  font-size: 0.85rem;
  font-weight: 600;
}
.error {
  color: var(--danger-text);
  font-size: 0.85rem;
}
h1 {
  margin-bottom: 1.5rem;
}
h2 {
  margin-top: 2.5rem;
}
.notice {
  border: 1px solid var(--border);
  background: var(--surface);
  border-radius: 10px;
  padding: 1rem;
  max-width: 32rem;
  margin-bottom: 1rem;
  color: var(--text-dim);
  font-size: 0.9rem;
}
.notice.warning {
  border-color: var(--warning-text);
  background: var(--warning-bg);
  color: var(--warning-text);
}
.revealed {
  border: 1px solid var(--success-text);
  background: var(--success-bg);
  border-radius: 10px;
  padding: 0.75rem 1rem;
  margin-bottom: 1rem;
  display: flex;
  align-items: center;
  gap: 0.75rem;
  flex-wrap: wrap;
  color: var(--success-text);
  font-size: 0.9rem;
}
.revealed code {
  color: var(--text);
}
.hint {
  font-size: 0.9rem;
  color: var(--text-dim);
  max-width: 40rem;
  margin-bottom: 1.5rem;
  line-height: 1.55;
}
.users-table {
  border-collapse: collapse;
  width: 100%;
}
.users-table th {
  text-align: left;
  padding: 0.6rem 0.7rem;
  font-family: var(--font-ui);
  font-size: 0.72rem;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--text-faint);
  border-bottom: 1px solid var(--border);
}
.users-table td {
  text-align: left;
  padding: 0.6rem 0.7rem;
  border-bottom: 1px solid var(--border);
}
.row-actions {
  display: flex;
  gap: 0.4rem;
  align-items: center;
}
.button {
  display: inline-block;
  padding: 0.4rem 0.75rem;
  border: 1px solid var(--border-strong);
  border-radius: 7px;
  text-decoration: none;
  color: var(--text);
  font-size: 0.85rem;
  font-weight: 600;
}
.button:hover {
  background: var(--surface-hover);
}
.button.disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.hint-inline {
  color: var(--text-faint);
  font-size: 0.85rem;
}
.users-table code {
  font-family: var(--font-mono);
}
</style>
