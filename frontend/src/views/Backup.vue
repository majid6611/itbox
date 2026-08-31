<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useBackupStore } from '../stores/backup'
import type { BackupConfig } from '../api/types'

const backup = useBackupStore()
const busy = ref(false)
const saved = ref(false)

const form = reactive<BackupConfig>({
  destination: 'local',
  aws_access_key_id: '',
  aws_secret_access_key: '',
  aws_bucket: '',
  aws_region: '',
  schedule: 'off',
})

onMounted(async () => {
  await backup.fetchAll()
  if (backup.config) Object.assign(form, backup.config)
})

const lastBackup = computed(() => backup.runs.find((r) => r.kind === 'backup'))

async function save() {
  busy.value = true
  saved.value = false
  try {
    await backup.saveConfig(form)
    saved.value = true
    // The server never echoes the secret back — keep whatever we just
    // sent showing in the field instead of it going blank after reload.
    const sentSecret = form.aws_secret_access_key
    if (backup.config) Object.assign(form, backup.config)
    if (sentSecret) form.aws_secret_access_key = sentSecret
  } finally {
    busy.value = false
  }
}

async function runNow() {
  busy.value = true
  try {
    await backup.runNow()
  } finally {
    busy.value = false
  }
}

async function restoreNow() {
  if (!confirm('Restore files from the backup into WebDAV? This only adds/updates files — it never deletes anything already there.')) return
  busy.value = true
  try {
    await backup.restoreNow()
  } finally {
    busy.value = false
  }
}

function formatTime(iso: string) {
  return new Date(iso).toLocaleString()
}
</script>

<template>
  <div>
    <h1>Backup</h1>
    <p class="hint">
      Backs up everyone's WebDAV files to S3-compatible storage — either the
      backup bucket built into this platform (no setup needed), or your own
      AWS S3 bucket.
    </p>

    <div class="status-row">
      <span v-if="!lastBackup" class="pill pill-neutral">No backups yet</span>
      <span v-else class="pill" :class="lastBackup.status === 'success' ? 'pill-good' : lastBackup.status === 'error' ? 'pill-bad' : 'pill-warn'">
        {{ lastBackup.status === 'running' ? 'Backing up…' : lastBackup.status === 'success' ? 'Last backup succeeded' : 'Last backup failed' }}
      </span>
      <span v-if="lastBackup" class="hint-inline">{{ formatTime(lastBackup.started_at) }}</span>
      <button :disabled="busy" @click="runNow">{{ busy ? 'Working…' : 'Back up now' }}</button>
      <button class="secondary" :disabled="busy" @click="restoreNow">Restore from backup</button>
    </div>
    <p v-if="lastBackup?.status === 'error' && lastBackup.error_message" class="error-message">
      {{ lastBackup.error_message }}
    </p>

    <form @submit.prevent="save" class="config-form card">
      <h2>Destination</h2>
      <label class="radio">
        <input type="radio" v-model="form.destination" value="local" />
        This platform's own backup storage (default, nothing to set up)
      </label>
      <label class="radio">
        <input type="radio" v-model="form.destination" value="aws" />
        My own AWS S3 bucket
      </label>

      <template v-if="form.destination === 'aws'">
        <label>
          AWS access key ID
          <input v-model="form.aws_access_key_id" type="text" required />
        </label>
        <label>
          AWS secret access key
          <input v-model="form.aws_secret_access_key" type="password" placeholder="Leave blank to keep the current one" />
        </label>
        <label>
          Bucket name
          <input v-model="form.aws_bucket" type="text" required />
        </label>
        <label>
          Region
          <input v-model="form.aws_region" type="text" placeholder="e.g. us-east-1" required />
        </label>
      </template>

      <h2>Schedule</h2>
      <label>
        <select v-model="form.schedule">
          <option value="off">Off (only when I click "Back up now")</option>
          <option value="daily">Daily</option>
          <option value="weekly">Weekly</option>
        </select>
      </label>

      <button type="submit" :disabled="busy">{{ busy ? 'Saving…' : 'Save' }}</button>
      <span v-if="saved" class="saved">Saved.</span>
    </form>

    <h2>History</h2>
    <table class="users-table" v-if="backup.runs.length">
      <thead>
        <tr>
          <th>Started</th>
          <th>Type</th>
          <th>Status</th>
          <th>Details</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="r in backup.runs" :key="r.started_at + r.kind">
          <td>{{ formatTime(r.started_at) }}</td>
          <td>{{ r.kind === 'backup' ? 'Backup' : 'Restore' }}</td>
          <td><span class="pill" :class="r.status === 'success' ? 'pill-good' : r.status === 'error' ? 'pill-bad' : 'pill-warn'">{{ r.status }}</span></td>
          <td>{{ r.error_message || '—' }}</td>
        </tr>
      </tbody>
    </table>
    <p v-else class="hint">No backups have run yet.</p>
  </div>
</template>

<style scoped>
h1 {
  margin-bottom: 0.75rem;
}
h2 {
  margin-top: 2rem;
}
.hint {
  font-size: 0.9rem;
  color: var(--text-dim);
  max-width: 40rem;
  margin-bottom: 1rem;
  line-height: 1.55;
}
.hint-inline {
  font-size: 0.85rem;
  color: var(--text-faint);
}
.status-row {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin-bottom: 1rem;
  flex-wrap: wrap;
}
.error-message {
  color: var(--danger-text);
  font-size: 0.9rem;
  max-width: 40rem;
}
.config-form {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  max-width: 28rem;
  padding: 1.1rem;
  margin-bottom: 1.5rem;
}
.config-form h2 {
  font-size: 0.95rem;
  margin: 0.5rem 0 0;
}
.config-form label {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  font-size: 0.85rem;
  font-weight: 500;
}
.config-form label.radio {
  flex-direction: row;
  align-items: center;
  gap: 0.5rem;
  font-weight: 400;
}
.config-form button {
  align-self: flex-start;
  margin-top: 0.25rem;
}
.saved {
  color: var(--success-text);
  font-size: 0.85rem;
  font-weight: 600;
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
</style>
