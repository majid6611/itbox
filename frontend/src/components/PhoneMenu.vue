<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api, ApiError } from '../api/client'
import { useVoipStore } from '../stores/voip'
import type { PhoneMenu } from '../api/types'

const voip = useVoipStore()

const menu = ref<PhoneMenu | null>(null)
const form = reactive({ enabled: false, recorder: '', options: [] as { digit: string; label: string; targets: string[] }[] })
const saving = ref(false)
const saved = ref(false)
const error = ref('')
const uploading = ref(false)
const uploadError = ref('')
// Changes the preview player's address after an upload, so it reloads.
const greetingStamp = ref(Date.now())

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

async function load() {
  const m = await api.get<PhoneMenu>('/voip/menu')
  menu.value = m
  form.enabled = m.enabled
  form.recorder = m.recorder
  form.options = (m.options ?? []).map((o) => ({ ...o, targets: [...o.targets] }))
}
onMounted(load)

const usedDigits = computed(() => new Set(form.options.map((o) => o.digit)))
const freeDigits = computed(() => '123456789'.split('').filter((d) => !usedDigits.value.has(d)))

function addKey() {
  const digit = freeDigits.value[0]
  if (!digit) return
  form.options.push({ digit, label: '', targets: [] })
  form.options.sort((a, b) => a.digit.localeCompare(b.digit))
}
function removeKey(digit: string) {
  form.options = form.options.filter((o) => o.digit !== digit)
}
function toggleTarget(key: { targets: string[] }, number: string) {
  const i = key.targets.indexOf(number)
  if (i >= 0) key.targets.splice(i, 1)
  else key.targets.push(number)
}

async function save() {
  saving.value = true
  saved.value = false
  error.value = ''
  try {
    await api.put('/voip/menu', {
      enabled: form.enabled,
      recorder: form.recorder,
      options: form.options.map((o) => ({ digit: o.digit, label: o.label.trim(), targets: o.targets })),
    })
    await load()
    saved.value = true
  } catch (e) {
    error.value = errText(e, 'Could not save the menu')
  } finally {
    saving.value = false
  }
}

async function upload(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  uploading.value = true
  uploadError.value = ''
  try {
    const body = new FormData()
    body.append('file', file)
    await api.upload('/voip/menu/greeting', body)
    greetingStamp.value = Date.now()
    await load()
  } catch (e) {
    uploadError.value = errText(e, 'Could not use that file')
  } finally {
    uploading.value = false
    input.value = ''
  }
}
</script>

<template>
  <div v-if="menu" class="menu">
    <p class="hint">
      When someone calls your company number from outside, they hear your greeting and press a key — "for IT support
      press 1". It is one level: a key rings those people straight away. Pressing <strong>0</strong>, or pressing
      nothing, rings the main number.
    </p>

    <div class="card block">
      <h3>1. The greeting</h3>
      <div v-if="menu.has_greeting" class="player">
        <audio :src="`/api/voip/menu/greeting?t=${greetingStamp}`" controls preload="none"></audio>
        <span class="hint small">Recorded {{ menu.greeting_updated_at ? new Date(menu.greeting_updated_at).toLocaleString() : '' }}</span>
      </div>
      <p v-else class="hint">No greeting yet.</p>
      <p class="hint small">
        Say something like: "Thank you for calling Acme. For IT support press 1. For accounting press 2. To reach the
        front desk press 0."
      </p>
      <label class="upload">
        <span>{{ uploading ? 'Converting…' : menu.has_greeting ? 'Replace with a file' : 'Upload a recording' }}</span>
        <input type="file" accept="audio/*,.mp3,.wav,.m4a,.ogg,.flac,.webm" :disabled="uploading" @change="upload" />
      </label>
      <span class="hint small">WAV, MP3, M4A, OGG or FLAC, up to 2 minutes.</span>
      <p v-if="uploadError" class="error">{{ uploadError }}</p>

      <p class="hint small or">…or record it with a phone:</p>
      <label>
        Allow this extension to record the greeting
        <select v-model="form.recorder">
          <option value="">Nobody (upload a file instead)</option>
          <option v-for="e in voip.extensions" :key="e.number" :value="e.number">{{ e.number }} — {{ e.name }}</option>
        </select>
      </label>
      <p v-if="form.recorder" class="hint small">
        After saving, pick up that phone and dial <code>{{ menu.record_code }}</code>. After the beep, speak, then press
        <kbd>#</kbd> (or just stop talking for 5 seconds). It plays your recording back. Dial again to redo it — each
        time replaces the greeting. Nobody else's phone can do this.
      </p>
    </div>

    <div class="card block">
      <h3>2. What each key does</h3>
      <p v-if="!form.options.length" class="hint">No keys yet.</p>
      <div v-for="o in form.options" :key="o.digit" class="key">
        <div class="key-head">
          <span class="digit">{{ o.digit }}</span>
          <input v-model="o.label" type="text" placeholder="e.g. IT support" maxlength="40" />
          <button type="button" class="secondary small" @click="removeKey(o.digit)">Remove</button>
        </div>
        <div class="targets">
          <span class="hint small">Rings:</span>
          <label v-for="e in voip.extensions" :key="e.number" class="chip" :class="{ on: o.targets.includes(e.number) }">
            <input type="checkbox" :checked="o.targets.includes(e.number)" @change="toggleTarget(o, e.number)" />
            {{ e.name }} <span class="num">{{ e.number }}</span>
          </label>
          <span v-if="!voip.extensions.length" class="hint small">Add extensions first.</span>
        </div>
      </div>
      <button type="button" class="secondary" :disabled="!freeDigits.length" @click="addKey">+ Add a key</button>
      <p class="hint small">
        Pick one person or several (all of them ring together). If nobody answers, the caller can leave a voicemail.
      </p>
    </div>

    <label class="check enable">
      <input v-model="form.enabled" type="checkbox" />
      Answer outside callers with this menu
    </label>
    <div class="save-row">
      <button :disabled="saving" @click="save">{{ saving ? 'Saving…' : 'Save' }}</button>
      <span v-if="saved" class="saved">Saved.</span>
      <span v-if="error" class="error">{{ error }}</span>
    </div>
    <p v-if="voip.settings && voip.settings.trunk_status === 'off'" class="hint small">
      Callers from outside only arrive once an outside line is set up above. Until then you can still hear it from any
      office phone by dialing <code>*50</code>.
    </p>
  </div>
</template>

<style scoped>
.menu {
  max-width: 44rem;
}
.hint {
  font-size: 0.9rem;
  color: var(--text-dim);
  line-height: 1.55;
}
.hint.small {
  font-size: 0.8rem;
  margin: 0.4rem 0;
}
.hint.or {
  margin-top: 1rem;
}
.block {
  padding: 1rem 1.2rem;
  margin: 0.9rem 0;
}
.block h3 {
  margin: 0 0 0.6rem;
  font-size: 0.95rem;
}
.player {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  flex-wrap: wrap;
}
.upload {
  display: inline-block;
  margin-top: 0.5rem;
  cursor: pointer;
  border: 1px solid var(--border-strong);
  border-radius: 7px;
  padding: 0.4rem 0.9rem;
  font-size: 0.88rem;
  font-weight: 500;
}
.upload input {
  display: none;
}
.key {
  border-top: 1px solid var(--border);
  padding: 0.7rem 0;
}
.key:first-of-type {
  border-top: none;
}
.key-head {
  display: flex;
  align-items: center;
  gap: 0.6rem;
}
.digit {
  width: 1.8rem;
  height: 1.8rem;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: var(--accent);
  color: white;
  font-weight: 700;
  flex-shrink: 0;
}
.key-head input {
  flex: 1;
}
.targets {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.4rem;
  margin: 0.5rem 0 0 2.4rem;
}
.chip {
  display: inline-flex;
  align-items: center;
  gap: 0.35rem;
  border: 1px solid var(--border);
  border-radius: 999px;
  padding: 0.15rem 0.7rem;
  font-size: 0.82rem;
  cursor: pointer;
}
.chip.on {
  background: var(--accent-soft);
  border-color: var(--accent);
  color: var(--accent);
}
.chip input {
  display: none;
}
.num {
  color: var(--text-faint);
}
.enable {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin: 1rem 0 0.6rem;
  font-weight: 600;
}
.save-row {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}
.error {
  color: var(--danger-text);
  font-size: 0.88rem;
}
.saved {
  color: var(--success-text);
  font-size: 0.88rem;
}
.small {
  font-size: 0.78rem;
  padding: 0.15rem 0.55rem;
}
kbd {
  border: 1px solid var(--border-strong);
  border-radius: 4px;
  padding: 0 0.35rem;
  font-size: 0.8rem;
}
</style>
