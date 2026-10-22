<script setup lang="ts">
import { computed, onMounted } from 'vue'
import FileShareGuide from '../components/FileShareGuide.vue'
import { useModulesStore } from '../stores/modules'

const modules = useModulesStore()
onMounted(() => modules.fetchAll())

const hostname = computed(() => modules.links['fileshare-webdav']?.[0]?.hostname ?? '')
const isPrivate = computed(() => modules.statuses['fileshare-webdav']?.visibility === 'private')
</script>

<template>
  <div>
    <h1>File Share</h1>
    <p class="subtitle">
      This is what to tell your staff. Employees also see this same guide under <strong>Files</strong> in their portal.
    </p>
    <p v-if="isPrivate" class="hint">
      This module is set to <strong>Private</strong>: it works from the office network and over the VPN, not from the internet.
    </p>
    <FileShareGuide v-if="hostname" :hostname="hostname" />
    <p v-else class="hint">Install and start the File Share module first.</p>
  </div>
</template>

<style scoped>
h1 {
  margin-bottom: 0.35rem;
}
.subtitle {
  color: var(--text-dim);
  font-size: 0.92rem;
  margin: 0 0 1.25rem;
}
.hint {
  color: var(--text-dim);
  font-size: 0.9rem;
}
</style>
