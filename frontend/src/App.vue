<script setup lang="ts">
import { useAuthStore } from './stores/auth'
import { useRouter } from 'vue-router'

const auth = useAuthStore()
const router = useRouter()

async function handleLogout() {
  await auth.logout()
  router.push({ name: 'login' })
}
</script>

<template>
  <div class="app-shell">
    <header v-if="auth.email" class="topbar">
      <nav>
        <router-link to="/">Dashboard</router-link>
        <router-link to="/modules">Module Store</router-link>
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
