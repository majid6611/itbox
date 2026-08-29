import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import Login from '../views/Login.vue'
import Dashboard from '../views/Dashboard.vue'
import ModuleStore from '../views/ModuleStore.vue'
import Users from '../views/Users.vue'
import Vpn from '../views/Vpn.vue'
import Settings from '../views/Settings.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: Login },
    { path: '/', name: 'dashboard', component: Dashboard },
    { path: '/modules', name: 'modules', component: ModuleStore },
    { path: '/users', name: 'users', component: Users },
    { path: '/vpn', name: 'vpn', component: Vpn },
    { path: '/settings', name: 'settings', component: Settings },
  ],
})

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  if (!auth.checked) {
    await auth.fetchMe()
  }
  if (to.name !== 'login' && !auth.email) {
    return { name: 'login' }
  }
  if (to.name === 'login' && auth.email) {
    return { name: 'dashboard' }
  }
  return true
})

export default router
