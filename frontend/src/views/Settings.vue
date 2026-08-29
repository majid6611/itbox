<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useSettingsStore } from '../stores/settings'

const settings = useSettingsStore()
const domain = ref('')
const busy = ref(false)
const saved = ref(false)

onMounted(async () => {
  await settings.fetch()
  domain.value = settings.baseDomain
})

async function save() {
  busy.value = true
  saved.value = false
  try {
    await settings.save(domain.value)
    saved.value = true
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div>
    <h1>Settings</h1>

    <form @submit.prevent="save" class="settings-form">
      <label>
        Domain name
        <input v-model="domain" type="text" placeholder="company.example.com" required />
      </label>
      <p class="hint">
        This is the web address people use to reach the services you install (fileshare,
        VPN, etc.) — each one gets its own address under it, like
        <code>fileshare.{{ domain || 'company.example.com' }}</code>. Leave it as
        <code>localhost</code> if you're only using this on one computer. To let people
        connect from outside, point a real domain name at this server first, then set it
        here.
      </p>
      <p class="hint warning">
        Changing this only affects services you install <strong>after</strong> saving —
        anything already installed keeps using the old address until it's reinstalled.
      </p>
      <button type="submit" :disabled="busy || settings.loading">{{ busy ? 'Saving…' : 'Save' }}</button>
      <span v-if="saved" class="saved">Saved.</span>
    </form>
  </div>
</template>

<style scoped>
.settings-form {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  max-width: 28rem;
  border: 1px solid #333;
  border-radius: 8px;
  padding: 1rem;
}
.settings-form label {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  font-size: 0.9rem;
}
.settings-form input {
  font-family: monospace;
}
.hint {
  font-size: 0.85rem;
  opacity: 0.8;
  margin: 0;
}
.hint.warning {
  opacity: 1;
  color: #b08800;
}
.hint code {
  font-family: monospace;
  background: rgba(255, 255, 255, 0.08);
  padding: 0.05rem 0.3rem;
  border-radius: 4px;
}
.settings-form button {
  align-self: flex-start;
  margin-top: 0.25rem;
}
.saved {
  color: #1a7f37;
  font-size: 0.85rem;
}
</style>
