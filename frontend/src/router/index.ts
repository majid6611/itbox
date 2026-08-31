import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { usePortalStore } from '../stores/portal'
import Login from '../views/Login.vue'
import Dashboard from '../views/Dashboard.vue'
import ModuleStore from '../views/ModuleStore.vue'
import Users from '../views/Users.vue'
import Vpn from '../views/Vpn.vue'
import Settings from '../views/Settings.vue'
import Backup from '../views/Backup.vue'
import WikiPermissions from '../views/WikiPermissions.vue'
import ComputeMesh from '../views/ComputeMesh.vue'
import PortalLogin from '../views/portal/PortalLogin.vue'
import Wiki from '../views/portal/Wiki.vue'
import Chat from '../views/portal/Chat.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: Login },
    { path: '/', name: 'dashboard', component: Dashboard },
    { path: '/modules', name: 'modules', component: ModuleStore },
    { path: '/users', name: 'users', component: Users },
    { path: '/vpn', name: 'vpn', component: Vpn },
    { path: '/settings', name: 'settings', component: Settings },
    { path: '/backup', name: 'backup', component: Backup },
    { path: '/wiki-permissions', name: 'wiki-permissions', component: WikiPermissions },
    { path: '/compute-mesh', name: 'compute-mesh', component: ComputeMesh },
    { path: '/portal/login', name: 'portal-login', component: PortalLogin },
    { path: '/portal/wiki/:pathMatch(.*)*', name: 'portal-wiki', component: Wiki },
    { path: '/portal/chat', name: 'portal-chat', component: Chat },
  ],
})

// The employee portal has its own login and session, entirely separate
// from the admin's — so it gets its own guard, scoped to /portal/*, that
// never touches or is touched by the admin auth store.
router.beforeEach(async (to) => {
  if (to.path.startsWith('/portal')) {
    const portal = usePortalStore()
    if (!portal.checked) {
      await portal.fetchMe()
    }
    if (to.name !== 'portal-login' && !portal.username) {
      return { name: 'portal-login' }
    }
    if (to.name === 'portal-login' && portal.username) {
      return { name: 'portal-wiki', params: { pathMatch: [] } }
    }
    return true
  }

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
