<script setup lang="ts">
import { computed, watch } from 'vue'
import { useAuthStore } from './stores/auth'
import { usePortalStore } from './stores/portal'
import { usePortalModulesStore } from './stores/portalModules'
import { useChatStore } from './stores/chat'
import { useRoute, useRouter } from 'vue-router'
import Icon from './components/Icon.vue'

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
      <div class="brand">
        <Icon name="mark" :size="20" />
        <span>IT Platform</span>
      </div>
      <nav>
        <router-link v-if="portalModules.modules.wiki" :to="{ name: 'portal-wiki', params: { pathMatch: [] } }">
          <Icon name="wiki" :size="16" /> Wiki
        </router-link>
        <router-link v-if="portalModules.modules.chat" :to="{ name: 'portal-chat' }" class="nav-link-with-badge">
          <Icon name="chat" :size="16" /> Chat
          <span v-if="chat.hasUnread" class="nav-badge" aria-label="Unread messages"></span>
        </router-link>
        <router-link v-if="portalModules.calendarAvailable" :to="{ name: 'portal-calendar' }">
          <Icon name="calendar" :size="16" /> Calendar
        </router-link>
      </nav>
      <div class="account">
        <span>{{ portal.username }}</span>
        <button class="secondary" @click="handlePortalLogout"><Icon name="logout" :size="15" /> Log out</button>
      </div>
    </header>
    <header v-else-if="!inPortal && auth.email" class="topbar admin">
      <div class="brand">
        <Icon name="mark" :size="20" />
        <span>IT Platform</span>
      </div>
      <nav>
        <router-link to="/"><Icon name="dashboard" :size="16" /> Dashboard</router-link>
        <router-link to="/modules"><Icon name="modules" :size="16" /> Module Store</router-link>
        <router-link to="/settings"><Icon name="settings" :size="16" /> Settings</router-link>
      </nav>
      <div class="account">
        <span>{{ auth.email }}</span>
        <button class="secondary" @click="handleLogout"><Icon name="logout" :size="15" /> Log out</button>
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
  align-items: center;
  gap: 2.5rem;
  padding: 0.75rem 1.75rem;
  font-family: var(--font-ui);
  font-size: 0.9rem;
}
.topbar:not(.admin) {
  background: var(--surface);
  border-bottom: 1px solid var(--border);
}
.brand {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-weight: 700;
  font-size: 0.95rem;
  color: var(--text);
}
.topbar.admin .brand {
  color: var(--admin-nav-text);
}
.brand .icon {
  color: var(--accent);
}
.topbar nav {
  display: flex;
  align-items: center;
  gap: 0.35rem;
  flex: 1;
}
.topbar nav a {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
}
.topbar:not(.admin) nav a {
  color: var(--text-dim);
  font-weight: 500;
  padding: 0.4rem 0.7rem;
  border-radius: 7px;
}
.topbar:not(.admin) nav a:hover {
  background: var(--surface-hover);
}
.topbar:not(.admin) nav a.router-link-active {
  color: var(--accent);
  background: var(--accent-soft);
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
.topbar.admin .account .secondary {
  background: transparent;
  color: var(--admin-nav-text-dim);
  border-color: var(--admin-nav-active-bg);
}
.topbar.admin .account .secondary:hover {
  background: var(--admin-nav-active-bg);
  color: var(--admin-nav-text);
}

.account {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}
.account button {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  font-size: 0.82rem;
  padding: 0.4rem 0.75rem;
}
main {
  padding: 2rem 1.75rem;
  max-width: 72rem;
  margin: 0 auto;
}
</style>
