<script setup lang="ts">
import { computed } from 'vue'
import { useAuthStore } from './stores/auth'
import { usePortalStore } from './stores/portal'
import { useRoute, useRouter } from 'vue-router'

const auth = useAuthStore()
const portal = usePortalStore()
const route = useRoute()
const router = useRouter()

const inPortal = computed(() => route.path.startsWith('/portal'))

async function handleLogout() {
  await auth.logout()
  router.push({ name: 'login' })
}

async function handlePortalLogout() {
  await portal.logout()
  router.push({ name: 'portal-login' })
}
</script>

<template>
  <div class="app-shell">
    <header v-if="inPortal && portal.username" class="topbar">
      <nav>
        <router-link :to="{ name: 'portal-wiki', params: { pathMatch: [] } }">Wiki</router-link>
      </nav>
      <div class="account">
        <span>{{ portal.username }}</span>
        <button @click="handlePortalLogout">Log out</button>
      </div>
    </header>
    <header v-else-if="!inPortal && auth.email" class="topbar">
      <nav>
        <router-link to="/">Dashboard</router-link>
        <router-link to="/modules">Module Store</router-link>
        <router-link to="/wiki-permissions">Wiki Permissions</router-link>
        <router-link to="/settings">Settings</router-link>
      </nav>
      <div class="account">
        <span>{{ auth.email }}</span>
        <button @click="handleLogout">Log out</button>
      </div>
    </header>
    <main>
      <router-view />
    </main>
  </div>
</template>

<style scoped>
.topbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0.75rem 1.5rem;
  border-bottom: 1px solid #333;
}
.topbar nav a {
  margin-right: 1rem;
}
.account {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}
main {
  padding: 1.5rem;
}
</style>
