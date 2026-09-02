<script setup lang="ts">
// A small hand-authored set, not an icon library — just the handful this
// app actually needs (nav + a couple of status/empty-state spots), kept
// as plain geometric primitives (rect/circle/line/simple paths) rather
// than dense bezier icon-font data.
withDefaults(defineProps<{ name: string; size?: number }>(), { size: 18 })
</script>

<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="2"
    stroke-linecap="round"
    stroke-linejoin="round"
    class="icon"
    aria-hidden="true"
  >
    <template v-if="name === 'dashboard'">
      <rect x="3" y="3" width="7.5" height="7.5" rx="1.8" />
      <rect x="13.5" y="3" width="7.5" height="7.5" rx="1.8" />
      <rect x="3" y="13.5" width="7.5" height="7.5" rx="1.8" />
      <rect x="13.5" y="13.5" width="7.5" height="7.5" rx="1.8" />
    </template>
    <template v-else-if="name === 'modules'">
      <rect x="3.5" y="7.5" width="17" height="13" rx="1.8" />
      <path d="M3.5 7.5L12 3.5l8.5 4" />
      <line x1="12" y1="7.5" x2="12" y2="20.5" />
    </template>
    <template v-else-if="name === 'settings'">
      <circle cx="12" cy="12" r="3.4" />
      <circle cx="12" cy="12" r="8.2" stroke-dasharray="2 3.4" />
    </template>
    <template v-else-if="name === 'wiki'">
      <path d="M5.5 4A1.8 1.8 0 003.7 5.8v14A1.8 1.8 0 015.5 18H18V4H5.5z" />
      <line x1="7.5" y1="7.5" x2="13" y2="7.5" />
      <line x1="7.5" y1="11.2" x2="13" y2="11.2" />
    </template>
    <template v-else-if="name === 'chat'">
      <path d="M4 5.5A1.5 1.5 0 015.5 4h13A1.5 1.5 0 0120 5.5v10a1.5 1.5 0 01-1.5 1.5H9l-4.5 4v-4H5.5A1.5 1.5 0 014 15.5v-10z" />
    </template>
    <template v-else-if="name === 'calendar'">
      <rect x="3.5" y="5" width="17" height="15" rx="1.8" />
      <line x1="3.5" y1="9.5" x2="20.5" y2="9.5" />
      <line x1="7.5" y1="3" x2="7.5" y2="6.5" />
      <line x1="16.5" y1="3" x2="16.5" y2="6.5" />
    </template>
    <template v-else-if="name === 'logout'">
      <path d="M9.5 4.5h-4a1.5 1.5 0 00-1.5 1.5v12a1.5 1.5 0 001.5 1.5h4" />
      <line x1="20.5" y1="12" x2="9.5" y2="12" />
      <polyline points="16 7.5 20.5 12 16 16.5" />
    </template>
    <template v-else-if="name === 'mark'">
      <rect x="3" y="3" width="8" height="8" rx="2" />
      <rect x="13" y="3" width="8" height="8" rx="2" fill="currentColor" stroke="none" />
      <rect x="3" y="13" width="8" height="8" rx="2" fill="currentColor" stroke="none" />
      <rect x="13" y="13" width="8" height="8" rx="2" />
    </template>
  </svg>
</template>

<style scoped>
.icon {
  flex-shrink: 0;
}
</style>
