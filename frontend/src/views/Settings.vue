<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useSettingsStore } from '../stores/settings'
import { ApiError } from '../api/client'
import type { ThemeName } from '../api/types'

const settings = useSettingsStore()
const domain = ref('')
const theme = ref<ThemeName>('slate')
const busy = ref(false)
const saved = ref(false)
const error = ref('')

const THEMES: { id: ThemeName; name: string; blurb: string; swatches: string[] }[] = [
  { id: 'slate', name: 'Slate & Signal', blurb: 'Cool, precise, status-first.', swatches: ['#0E7C86', '#12202B', '#F5F7F9'] },
  { id: 'stone', name: 'Warm Stone & Plum', blurb: 'Warm, calm, human.', swatches: ['#6B3F5E', '#EFEDE7', '#2A2521'] },
]

onMounted(async () => {
  await settings.fetch()
  domain.value = settings.baseDomain
  theme.value = settings.theme
})

async function save() {
  busy.value = true
  saved.value = false
  error.value = ''
  try {
    await settings.save(domain.value, theme.value)
    saved.value = true
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : 'Could not save settings'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="settings-page">
    <h1>Settings</h1>

    <form @submit.prevent="save" class="settings-form">
      <section class="field-group">
        <label class="field-label" for="domain">Domain name</label>
        <input id="domain" v-model="domain" type="text" placeholder="company.example.com" required />
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
      </section>

      <section class="field-group">
        <span class="field-label">Theme</span>
        <p class="hint">Applies to both the admin and employee portals, for everyone.</p>
        <div class="theme-picker">
          <label v-for="t in THEMES" :key="t.id" class="theme-option" :class="{ active: theme === t.id }">
            <span class="theme-option-top">
              <input v-model="theme" type="radio" name="theme" :value="t.id" />
              <span class="swatches">
                <span v-for="c in t.swatches" :key="c" class="swatch" :style="{ background: c }"></span>
              </span>
            </span>
            <span class="theme-name">{{ t.name }}</span>
            <span class="theme-blurb">{{ t.blurb }}</span>
          </label>
        </div>
      </section>

      <div class="actions">
        <button type="submit" :disabled="busy || settings.loading">{{ busy ? 'Saving…' : 'Save' }}</button>
        <span v-if="saved" class="saved">Saved.</span>
        <span v-if="error" class="error">{{ error }}</span>
      </div>
    </form>
  </div>
</template>

<style scoped>
.settings-page h1 {
  margin-bottom: 1.25rem;
}
.settings-form {
  display: flex;
  flex-direction: column;
  gap: 1.75rem;
  max-width: 34rem;
}
.field-group {
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
}
.field-label {
  font-family: var(--font-ui);
  font-weight: 600;
  font-size: 0.9rem;
}
.field-group input[type='text'] {
  font-family: var(--font-mono);
  max-width: 22rem;
}
.hint {
  font-size: 0.85rem;
  color: var(--text-dim);
  margin: 0;
  line-height: 1.55;
}
.hint.warning {
  color: var(--warning-text);
}

.theme-picker {
  display: flex;
  gap: 0.75rem;
  flex-wrap: wrap;
}
.theme-option {
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
  width: 12.5rem;
  padding: 0.8rem;
  border: 1.5px solid var(--border);
  border-radius: 10px;
  cursor: pointer;
  background: var(--surface);
}
.theme-option:hover {
  border-color: var(--border-strong);
}
.theme-option.active {
  border-color: var(--accent);
  box-shadow: 0 0 0 1px var(--accent);
}
.theme-option-top {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.theme-option input {
  accent-color: var(--accent);
  margin: 0;
}
.swatches {
  display: flex;
  gap: 0.3rem;
}
.swatch {
  width: 20px;
  height: 20px;
  border-radius: 6px;
  border: 1px solid rgba(0, 0, 0, 0.12);
}
.theme-name {
  font-family: var(--font-ui);
  font-weight: 700;
  font-size: 0.88rem;
}
.theme-blurb {
  font-size: 0.78rem;
  color: var(--text-dim);
}

.actions {
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
  font-weight: 600;
}
</style>
