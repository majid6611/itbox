<script setup lang="ts">
import FileShareGuide from '../../components/FileShareGuide.vue'
import { usePortalStore } from '../../stores/portal'
import { usePortalModulesStore } from '../../stores/portalModules'

const portal = usePortalStore()
const modules = usePortalModulesStore()
</script>

<template>
  <div>
    <h1>Files</h1>
    <p class="subtitle">Your company file share — connect it once and it works like a normal folder on your computer.</p>
    <FileShareGuide v-if="modules.fileshareHostname" :hostname="modules.fileshareHostname" :username="portal.username ?? undefined" />
    <p v-else class="hint">The file share isn't set up yet — ask your administrator.</p>
  </div>
</template>

<style scoped>
h1 {
  margin-bottom: 0.35rem;
}
.subtitle {
  color: var(--text-dim);
  font-size: 0.92rem;
  margin: 0 0 1.5rem;
}
.hint {
  color: var(--text-dim);
}
</style>
