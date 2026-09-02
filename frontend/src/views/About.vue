<script setup lang="ts">
import { onMounted } from 'vue'
import { useSettingsStore } from '../stores/settings'

const settings = useSettingsStore()

onMounted(() => settings.fetch())

// Placeholder — replace with the real support contact before shipping to
// anyone outside this dev environment.
const SUPPORT_EMAIL = 'support@example.com'
</script>

<template>
  <div class="about-page">
    <h1>About</h1>
    <p class="version" v-if="settings.platformVersion">IT Platform v{{ settings.platformVersion }}</p>

    <div class="card section">
      <h2>Free and open source</h2>
      <p>
        IT Platform is free, open-source software — self-hosted, no per-seat fees, no vendor lock-in. You can run
        it, inspect it, and modify it for your own company without asking anyone's permission. The full source is
        on
        <a href="https://github.com/majid6611/itbox" target="_blank" rel="noopener">GitHub</a>.
      </p>
    </div>

    <div class="card section">
      <h2>Need help?</h2>
      <p>
        The software itself is free — if you'd rather have someone else handle setup, upgrades, or troubleshooting,
        paid support is available.
      </p>
      <p>
        <a :href="`mailto:${SUPPORT_EMAIL}`">{{ SUPPORT_EMAIL }}</a>
      </p>
    </div>
  </div>
</template>

<style scoped>
.about-page h1 {
  margin-bottom: 0.35rem;
}
.version {
  color: var(--text-dim);
  font-size: 0.85rem;
  margin: 0 0 1.5rem;
}
.section {
  padding: 1.1rem 1.2rem;
  max-width: 32rem;
  margin-bottom: 1.25rem;
}
.section h2 {
  font-size: 1rem;
  margin: 0 0 0.5rem;
}
.section p {
  font-size: 0.9rem;
  color: var(--text-dim);
  line-height: 1.6;
  margin: 0 0 0.6rem;
}
.section p:last-child {
  margin-bottom: 0;
}
</style>
