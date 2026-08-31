<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useWikiAdminStore } from '../stores/wikiAdmin'
import { useGroupsStore } from '../stores/groups'
import type { WikiPermissionRule } from '../api/types'

const wikiAdmin = useWikiAdminStore()
const groups = useGroupsStore()
const selectedPath = ref('')
const draft = ref<WikiPermissionRule[]>([])
const saving = ref(false)
const saved = ref(false)

onMounted(async () => {
  await Promise.all([wikiAdmin.fetchPages(), groups.fetchAll()])
})

async function selectPage(path: string) {
  selectedPath.value = path
  saved.value = false
  await wikiAdmin.fetchRules(path)
  draft.value = wikiAdmin.rules.map((r) => ({ ...r }))
}

function addRule() {
  const firstUnused = groups.groups.find((g) => !draft.value.some((r) => r.group === g.name))
  draft.value.push({ group: firstUnused?.name ?? '', access: 'read' })
}

function removeRule(i: number) {
  draft.value.splice(i, 1)
}

async function save() {
  saving.value = true
  saved.value = false
  try {
    await wikiAdmin.saveRules(selectedPath.value, draft.value)
    saved.value = true
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div>
    <h1>Wiki Permissions</h1>
    <p class="hint">
      A page with no rules is open to every logged-in employee. Add a rule to restrict a page to
      specific groups.
    </p>

    <div class="layout">
      <ul class="page-list">
        <li v-for="p in wikiAdmin.pages" :key="p.id">
          <button :class="{ active: p.path === selectedPath }" @click="selectPage(p.path)">
            {{ p.path }}
          </button>
        </li>
      </ul>
      <p v-if="!wikiAdmin.loading && wikiAdmin.pages.length === 0" class="hint">No wiki pages yet.</p>

      <div v-if="selectedPath" class="rules-panel">
        <h2>{{ selectedPath }}</h2>
        <div v-for="(rule, i) in draft" :key="i" class="rule-row">
          <select v-model="rule.group">
            <option v-for="g in groups.groups" :key="g.name" :value="g.name">{{ g.name }}</option>
          </select>
          <select v-model="rule.access">
            <option value="read">Read</option>
            <option value="write">Read &amp; write</option>
          </select>
          <button class="secondary" @click="removeRule(i)">Remove</button>
        </div>
        <button class="secondary" @click="addRule">+ Add group rule</button>
        <div class="save-row">
          <button :disabled="saving" @click="save">{{ saving ? 'Saving…' : 'Save' }}</button>
          <span v-if="saved" class="saved">Saved.</span>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
h1 {
  margin-bottom: 0.75rem;
}
.hint {
  font-size: 0.9rem;
  color: var(--text-dim);
  max-width: 40rem;
  margin-bottom: 1.5rem;
  line-height: 1.55;
}
.layout {
  display: flex;
  gap: 2rem;
}
.page-list {
  list-style: none;
  padding: 0;
  margin: 0;
  min-width: 16rem;
}
.page-list button {
  display: block;
  width: 100%;
  text-align: left;
  background: none;
  border: none;
  padding: 0.4rem 0.6rem;
  cursor: pointer;
  color: var(--text);
  font-weight: 400;
  border-radius: 6px;
  font-size: 0.88rem;
}
.page-list button:hover {
  background: var(--surface-hover);
}
.page-list button.active {
  background: var(--accent-soft);
  color: var(--accent);
  font-weight: 600;
}
.rules-panel {
  flex: 1;
  max-width: 28rem;
}
.rules-panel h2 {
  font-size: 1rem;
}
.rule-row {
  display: flex;
  gap: 0.5rem;
  margin-bottom: 0.5rem;
  align-items: center;
}
.save-row {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin-top: 1rem;
}
.saved {
  color: var(--success-text);
  font-size: 0.85rem;
  font-weight: 600;
}
</style>
