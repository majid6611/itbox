<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { usePhoneStore } from '../../stores/phone'

const phone = usePhoneStore()
const search = ref('')
const showPassword = ref(false)

// The server's own LAN IP (see the voip store's serverAddress).
const serverAddress = computed(() => phone.serverAddress || window.location.hostname)

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return phone.directory
  return phone.directory.filter((e) => e.name.toLowerCase().includes(q) || e.number.includes(q))
})

onMounted(() => phone.fetchAll())
</script>

<template>
  <div>
    <h1>Phone</h1>
    <p class="subtitle">Your office phone, and everyone's extension number.</p>

    <template v-if="phone.loaded">
      <div v-if="phone.mine" class="card mine">
        <h2>Your extension: {{ phone.mine.number }}</h2>
        <p class="hint">
          On a desk phone, or a free softphone app on your computer or mobile
          (<strong>Linphone</strong> or <strong>Zoiper</strong>), add a SIP account with these details:
        </p>
        <div class="setup-grid">
          <span>Server</span><code>{{ serverAddress }}</code>
          <span>Username</span><code>{{ phone.mine.number }}</code>
          <span>Password</span>
          <span>
            <code v-if="showPassword">{{ phone.mine.password }}</code>
            <button v-else type="button" class="secondary small" @click="showPassword = true">Show password</button>
          </span>
          <span>Transport</span><code>UDP</code>
        </div>
        <p class="hint small">
          Away from the office? Connect to the company VPN first, then the phone works the same way.
          Dial <code>*43</code> to hear yourself and check everything works.
        </p>
      </div>
      <div v-else class="notice">You don't have a phone extension yet — ask your admin for one.</div>

      <h2>Dialing</h2>
      <ul class="dial-list">
        <li><code>0</code> — the main number</li>
        <li><code>9</code> + number — an outside call</li>
        <li><code>*97</code> — your voicemail</li>
      </ul>

      <h2>Directory</h2>
      <input v-model="search" type="search" placeholder="Search by name or number" class="search" />
      <table v-if="filtered.length" class="dir-table">
        <tbody>
          <tr v-for="e in filtered" :key="e.number">
            <td>{{ e.name }}</td>
            <td><strong>{{ e.number }}</strong></td>
          </tr>
        </tbody>
      </table>
      <p v-else class="hint">{{ phone.directory.length ? 'Nobody matches that.' : 'No extensions yet.' }}</p>
    </template>
  </div>
</template>

<style scoped>
h1 {
  margin-bottom: 0.35rem;
}
h2 {
  margin-top: 2rem;
  font-size: 1.05rem;
}
.subtitle {
  color: var(--text-dim);
  margin-bottom: 1.5rem;
}
.mine {
  padding: 1.25rem 1.5rem;
  max-width: 36rem;
}
.mine h2 {
  margin-top: 0;
}
.hint {
  font-size: 0.9rem;
  color: var(--text-dim);
  line-height: 1.55;
  margin-bottom: 1rem;
}
.hint.small {
  font-size: 0.82rem;
  margin: 1rem 0 0;
}
.setup-grid {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 0.45rem 1.25rem;
  align-items: center;
  font-size: 0.9rem;
}
.setup-grid code {
  user-select: all;
}
button.small {
  font-size: 0.8rem;
  padding: 0.2rem 0.6rem;
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
.dial-list {
  padding-left: 1.2rem;
  font-size: 0.9rem;
  line-height: 1.8;
}
.search {
  max-width: 20rem;
  width: 100%;
  margin-bottom: 0.75rem;
}
.dir-table {
  border-collapse: collapse;
  width: 100%;
  max-width: 28rem;
}
.dir-table td {
  padding: 0.5rem 0.7rem;
  border-bottom: 1px solid var(--border);
}
.dir-table td:last-child {
  text-align: right;
}
</style>
