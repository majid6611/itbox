<script setup lang="ts">
import { computed, watch } from 'vue'
import { useAuthStore } from './stores/auth'
import { usePortalStore } from './stores/portal'
import { usePortalModulesStore } from './stores/portalModules'
import { useChatStore } from './stores/chat'
import { useRoute, useRouter } from 'vue-router'

const auth = useAuthStore()
const portal = usePortalStore()
const portalModules = usePortalModulesStore()
const chat = useChatStore()
const route = useRoute()
const router = useRouter()

const inPortal = computed(() => route.path.startsWith('/portal'))

// Only fetch once we know there's an employee session to fetch as — the
// portal auth guard runs before this component even mounts, so by the
// time portal.username is set, /api/portal/modules is callable.
watch(
  () => portal.username,
  (username) => {
    if (username && !portalModules.checked) portalModules.fetchAll()
  },
  { immediate: true },
)

// The chat WebSocket lives here, not in Chat.vue, so the nav badge and
// browser notifications keep working while an employee is on any other
// portal page — the whole point of a nav-level "signal" is noticing a
// message without having the chat page open in the first place.
watch(
  () => [portal.username, portalModules.modules.chat] as const,
  ([username, chatEnabled]) => {
    if (username && chatEnabled && !chat.wsConnected) {
      chat.connectWS(username)
    } else if ((!username || !chatEnabled) && chat.ws) {
      chat.disconnectWS()
    }
  },
)

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
        <router-link v-if="portalModules.modules.wiki" :to="{ name: 'portal-wiki', params: { pathMatch: [] } }">Wiki</router-link>
        <router-link v-if="portalModules.modules.chat" :to="{ name: 'portal-chat' }" class="nav-link-with-badge">
          Chat
          <span v-if="chat.hasUnread" class="nav-badge" aria-label="Unread messages"></span>
        </router-link>
      </nav>
      <div class="account">
        <span>{{ portal.username }}</span>
        <button @click="handlePortalLogout">Log out</button>
      </div>
    </header>
    <header v-else-if="!inPortal && auth.email" class="topbar admin">
      <nav>
        <router-link to="/">Dashboard</router-link>
        <router-link to="/modules">Module Store</router-link>
        <router-link to="/settings">Settings</router-link>
      </nav>
      <div class="account">
        <span>{{ auth.email }}</span>
        <button class="secondary" @click="handleLogout">Log out</button>
      </div>
    </header>
    <main :class="{ admin: !inPortal && auth.email }">
      <router-view />
    </main>
  </div>
</template>

<style scoped>
.topbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0.85rem 1.75rem;
  font-family: var(--font-ui);
  font-size: 0.9rem;
}
.topbar:not(.admin) {
  background: var(--surface);
  border-bottom: 1px solid var(--border);
}
.topbar nav {
  display: flex;
  align-items: center;
  gap: 1.5rem;
}
.topbar:not(.admin) nav a {
  color: var(--text-dim);
  font-weight: 500;
}
.topbar:not(.admin) nav a.router-link-active {
  color: var(--accent);
}
.nav-link-with-badge {
  position: relative;
}
.nav-badge {
  position: absolute;
  top: -3px;
  right: -8px;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--danger-text);
  border: 2px solid var(--surface);
}

/* The admin portal's own chrome, not the app's light/dark theme — a
 * deliberately darker header so "you're in the control panel" is legible
 * at a glance, distinct from the calmer employee portal above. */
.topbar.admin {
  background: var(--admin-nav-bg);
  border-bottom: 1px solid var(--admin-nav-border);
  color: var(--admin-nav-text);
}
.topbar.admin nav a {
  color: var(--admin-nav-text-dim);
  font-weight: 500;
  padding: 0.35rem 0.7rem;
  border-radius: 7px;
}
.topbar.admin nav a:hover {
  color: var(--admin-nav-text);
}
.topbar.admin nav a.router-link-active {
  background: var(--admin-nav-active-bg);
  color: var(--admin-nav-text);
}
.topbar.admin .account {
  color: var(--admin-nav-text-dim);
  font-size: 0.85rem;
}

.account {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}
main {
  padding: 2rem 1.75rem;
  max-width: 72rem;
  margin: 0 auto;
}
</style>
