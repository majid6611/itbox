<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useVoipStore } from '../stores/voip'
import { useUsersStore } from '../stores/users'
import { ApiError } from '../api/client'
import type { PhoneExtension } from '../api/types'
import PhoneMenu from '../components/PhoneMenu.vue'

const voip = useVoipStore()
const users = useUsersStore()

// The server's own LAN IP, as Asterisk detected it. Falls back to
// whatever address this panel was opened with only if the PBX isn't
// answering — "localhost" there would be useless on a phone.
const serverAddress = computed(() => voip.serverAddress || window.location.hostname)

const newExt = reactive({ number: '', name: '', username: '' })
const adding = ref(false)
const addError = ref('')
const setupFor = ref<PhoneExtension | null>(null)
const busy = reactive<Record<string, boolean>>({})

const trunk = reactive({
  trunk_enabled: false,
  trunk_host: '',
  trunk_username: '',
  trunk_password: '',
  trunk_caller_id: '',
  allow_international: false,
})
const savingTrunk = ref(false)
const trunkError = ref('')
const trunkSaved = ref(false)

// Employees who don't have an extension yet — one phone per person.
const unassignedUsers = computed(() =>
  users.users.filter((u) => !u.disabled && !voip.extensions.some((e) => e.username === u.username)),
)

function errText(e: unknown, fallback: string) {
  if (e instanceof ApiError) {
    try {
      return JSON.parse(e.message).detail ?? fallback
    } catch {
      return e.message || fallback
    }
  }
  return fallback
}

function loadTrunkForm() {
  const s = voip.settings
  if (!s) return
  trunk.trunk_enabled = s.trunk_enabled
  trunk.trunk_host = s.trunk_host
  trunk.trunk_username = s.trunk_username
  trunk.trunk_password = ''
  trunk.trunk_caller_id = s.trunk_caller_id
  trunk.allow_international = s.allow_international
}

// Live status dots without a manual refresh — cheap, it's one small
// request every few seconds while this page is open.
let timer: number | undefined
onMounted(async () => {
  await Promise.all([voip.fetchAll(), users.fetchAll().catch(() => {})])
  loadTrunkForm()
  timer = window.setInterval(() => {
    voip.fetchExtensions().catch(() => {})
  }, 5000)
})
onUnmounted(() => window.clearInterval(timer))

function pickUser() {
  const u = users.users.find((x) => x.username === newExt.username)
  if (u && !newExt.name) newExt.name = u.name || u.username
}

async function addExtension() {
  adding.value = true
  addError.value = ''
  try {
    const ext = await voip.create({ ...newExt })
    setupFor.value = ext
    newExt.number = ''
    newExt.name = ''
    newExt.username = ''
  } catch (e) {
    addError.value = errText(e, 'Could not add the extension')
  } finally {
    adding.value = false
  }
}

async function toggleMain(ext: PhoneExtension) {
  busy[ext.number] = true
  try {
    await voip.update(ext, { in_main_group: !ext.in_main_group })
  } finally {
    busy[ext.number] = false
  }
}

async function resetPassword(ext: PhoneExtension) {
  if (!confirm(`Give ${ext.number} a new password? The phone stops working until the new one is entered on it.`)) return
  busy[ext.number] = true
  try {
    await voip.resetPassword(ext.number)
    setupFor.value = voip.extensions.find((e) => e.number === ext.number) ?? null
  } finally {
    busy[ext.number] = false
  }
}

async function removeExtension(ext: PhoneExtension) {
  if (!confirm(`Remove extension ${ext.number} (${ext.name})?`)) return
  busy[ext.number] = true
  try {
    await voip.remove(ext.number)
    if (setupFor.value?.number === ext.number) setupFor.value = null
  } finally {
    busy[ext.number] = false
  }
}

async function saveTrunk() {
  savingTrunk.value = true
  trunkError.value = ''
  trunkSaved.value = false
  try {
    await voip.saveSettings({ ...trunk })
    loadTrunkForm()
    trunkSaved.value = true
  } catch (e) {
    trunkError.value = errText(e, 'Could not save the outside line')
  } finally {
    savingTrunk.value = false
  }
}

function personName(username: string | null) {
  if (!username) return ''
  const u = users.users.find((x) => x.username === username)
  return u?.name || username
}

const stateLabel: Record<string, string> = {
  online: 'online',
  in_call: 'on a call',
  offline: 'offline',
  unknown: 'unknown',
}
const stateClass: Record<string, string> = {
  online: 'pill-good',
  in_call: 'pill-warn',
  offline: 'pill-neutral',
  unknown: 'pill-neutral',
}
const resultLabel: Record<string, string> = {
  answered: 'answered',
  voicemail: 'voicemail',
  missed: 'missed',
  busy: 'busy',
  failed: 'failed',
}

function duration(s: number) {
  if (!s) return '—'
  const m = Math.floor(s / 60)
  return m ? `${m}m ${s % 60}s` : `${s}s`
}
</script>

<template>
  <div>
    <h1>
      Phone System
      <span v-if="!voip.loading" class="pill" :class="voip.pbxOnline ? 'pill-good' : 'pill-bad'">
        {{ voip.pbxOnline ? 'running' : 'not responding' }}
      </span>
    </h1>
    <p class="hint">
      Give each person a phone extension, then enter its details into their desk phone or a free
      softphone app (Linphone, Zoiper). Phones work on the office network, or from anywhere over the VPN.
    </p>

    <div v-if="setupFor" class="revealed">
      <div class="setup-grid">
        <strong class="setup-title">Phone setup for {{ setupFor.number }} — {{ setupFor.name }}</strong>
        <span>Server</span><code>{{ serverAddress }}</code>
        <span>Username</span><code>{{ setupFor.number }}</code>
        <span>Password</span><code>{{ setupFor.password }}</code>
        <span>Transport</span><code>UDP, port 5060</code>
      </div>
      <button type="button" class="secondary" @click="setupFor = null">Close</button>
    </div>

    <h2>Extensions</h2>
    <table v-if="voip.extensions.length" class="users-table">
      <thead>
        <tr>
          <th>Number</th>
          <th>Name</th>
          <th>Person</th>
          <th>Status</th>
          <th title="Rings when someone calls the main number">Main number</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="e in voip.extensions" :key="e.number">
          <td><strong>{{ e.number }}</strong></td>
          <td>{{ e.name }}</td>
          <td>{{ personName(e.username) }}<span v-if="!e.username" class="hint-inline">shared phone</span></td>
          <td><span class="pill" :class="stateClass[e.state]">{{ stateLabel[e.state] }}</span></td>
          <td>
            <input type="checkbox" :checked="e.in_main_group" :disabled="busy[e.number]" @change="toggleMain(e)" />
          </td>
          <td class="row-actions">
            <button class="secondary" @click="setupFor = e">Phone setup</button>
            <button class="secondary" :disabled="busy[e.number]" @click="resetPassword(e)">New password</button>
            <button class="secondary" :disabled="busy[e.number]" @click="removeExtension(e)">Remove</button>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-else-if="!voip.loading" class="hint">No extensions yet — add the first one below.</p>

    <form class="add-row" @submit.prevent="addExtension">
      <input v-model.trim="newExt.number" type="text" placeholder="Number, e.g. 101" inputmode="numeric" required class="num" />
      <select v-if="users.available" v-model="newExt.username" @change="pickUser">
        <option value="">Shared phone (no person)</option>
        <option v-for="u in unassignedUsers" :key="u.username" :value="u.username">
          {{ u.name || u.username }}
        </option>
      </select>
      <input v-model.trim="newExt.name" type="text" placeholder="Name shown on calls" required />
      <button :disabled="adding">{{ adding ? 'Adding…' : 'Add extension' }}</button>
    </form>
    <p v-if="addError" class="error">{{ addError }}</p>
    <p class="hint small">Numbers are 2–5 digits and can't start with 0 or 9 (those are for the main number and outside calls).</p>

    <h2>
      Outside line
      <span v-if="voip.settings" class="pill" :class="{
        'pill-good': voip.settings.trunk_status === 'connected',
        'pill-bad': voip.settings.trunk_status === 'not_connected',
        'pill-neutral': voip.settings.trunk_status === 'off',
      }">
        {{ voip.settings.trunk_status === 'connected' ? 'connected' : voip.settings.trunk_status === 'off' ? 'off' : 'not connected' }}
      </span>
    </h2>
    <p class="hint">
      To call real phone numbers, sign up with any SIP provider and copy the details they give you here.
      Without one, extensions can still call each other.
    </p>
    <form class="trunk-form" @submit.prevent="saveTrunk">
      <label class="check"><input v-model="trunk.trunk_enabled" type="checkbox" /> Use an outside line</label>
      <label>Provider server <input v-model.trim="trunk.trunk_host" type="text" placeholder="sip.provider.com" /></label>
      <label>Username <input v-model.trim="trunk.trunk_username" type="text" autocomplete="off" /></label>
      <label>
        Password
        <input
          v-model="trunk.trunk_password"
          type="password"
          autocomplete="new-password"
          :placeholder="voip.settings?.trunk_password_set ? 'Saved — leave empty to keep it' : ''"
        />
      </label>
      <label>Company phone number <input v-model.trim="trunk.trunk_caller_id" type="text" placeholder="Shown to people you call (optional)" /></label>
      <label class="check">
        <input v-model="trunk.allow_international" type="checkbox" /> Allow international calls
      </label>
      <p class="hint small">
        International calls are off by default, so a leaked phone password can't run up a big bill.
      </p>
      <div class="save-row">
        <button :disabled="savingTrunk">{{ savingTrunk ? 'Saving…' : 'Save' }}</button>
        <span v-if="trunkSaved" class="saved">Saved.</span>
        <span v-if="trunkError" class="error">{{ trunkError }}</span>
      </div>
    </form>

    <h2>Phone menu</h2>
    <PhoneMenu />

    <h2>What to dial</h2>
    <table class="users-table dial-table">
      <tbody>
        <tr><td><code>101</code>, <code>102</code>, …</td><td>Call a colleague</td></tr>
        <tr><td><code>0</code></td><td>The main number (rings everyone ticked above) — also what callers from outside get when they press 0 in the phone menu</td></tr>
        <tr><td><code>9</code> + number</td><td>Outside call, e.g. <code>95551234</code></td></tr>
        <tr><td><code>*97</code></td><td>Your voicemail</td></tr>
        <tr><td><code>*98</code></td><td>Main number's voicemail</td></tr>
        <tr><td><code>*50</code></td><td>Hear the phone menu, exactly as an outside caller does (only when it is switched on)</td></tr>
        <tr><td><code>*99</code></td><td>Record the phone menu greeting (only from the extension chosen in "Phone menu")</td></tr>
        <tr><td><code>*43</code></td><td>Echo test — hear yourself to check the phone works</td></tr>
      </tbody>
    </table>

    <h2>
      Recent calls
      <button type="button" class="secondary refresh" @click="voip.fetchCalls()">Refresh</button>
    </h2>
    <table v-if="voip.calls.length" class="users-table">
      <thead>
        <tr>
          <th>When</th>
          <th>From</th>
          <th>To</th>
          <th>Talk time</th>
          <th>Result</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(c, i) in voip.calls" :key="i">
          <td>{{ c.start ? new Date(c.start).toLocaleString() : '' }}</td>
          <td>{{ c.from }}</td>
          <td>{{ c.to }}</td>
          <td>{{ duration(c.talk_seconds) }}</td>
          <td>
            <span class="pill" :class="c.result === 'answered' ? 'pill-good' : c.result === 'voicemail' ? 'pill-neutral' : 'pill-warn'">
              {{ resultLabel[c.result] }}
            </span>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-else-if="!voip.loading" class="hint">No calls yet.</p>
  </div>
</template>

<style scoped>
h1 {
  margin-bottom: 0.75rem;
  display: flex;
  align-items: center;
  gap: 0.75rem;
}
h2 {
  margin-top: 2.5rem;
  display: flex;
  align-items: center;
  gap: 0.75rem;
}
.hint {
  font-size: 0.9rem;
  color: var(--text-dim);
  max-width: 40rem;
  margin-bottom: 1.25rem;
  line-height: 1.55;
}
.hint.small {
  font-size: 0.82rem;
  margin-top: 0.5rem;
}
.hint-inline {
  color: var(--text-faint);
  font-size: 0.85rem;
}
.revealed {
  border: 1px solid var(--success-text);
  background: var(--success-bg);
  border-radius: 10px;
  padding: 0.9rem 1rem;
  margin-bottom: 1rem;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
  color: var(--success-text);
  font-size: 0.9rem;
  max-width: 40rem;
}
.setup-grid {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 0.35rem 1rem;
}
.setup-title {
  grid-column: 1 / -1;
  margin-bottom: 0.25rem;
}
.setup-grid code {
  color: var(--text);
  user-select: all;
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
.users-table code {
  font-family: var(--font-mono);
}
.dial-table {
  max-width: 40rem;
}
.dial-table td:first-child {
  width: 10rem;
  white-space: nowrap;
}
.row-actions {
  display: flex;
  gap: 0.4rem;
  align-items: center;
}
.add-row {
  display: flex;
  gap: 0.5rem;
  margin-top: 1rem;
  flex-wrap: wrap;
}
.add-row .num {
  width: 9rem;
}
.trunk-form {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  max-width: 28rem;
}
.trunk-form label {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  font-size: 0.88rem;
  font-weight: 600;
}
.trunk-form label.check {
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
.refresh {
  font-size: 0.8rem;
  padding: 0.25rem 0.6rem;
}
</style>
