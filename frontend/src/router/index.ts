import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { usePortalStore } from '../stores/portal'
const Login = () => import('../views/Login.vue')
const Dashboard = () => import('../views/Dashboard.vue')
const ModuleStore = () => import('../views/ModuleStore.vue')
const Users = () => import('../views/Users.vue')
const Logs = () => import('../views/Logs.vue')
const Vpn = () => import('../views/Vpn.vue')
const Settings = () => import('../views/Settings.vue')
const About = () => import('../views/About.vue')
const Backup = () => import('../views/Backup.vue')
const WikiPermissions = () => import('../views/WikiPermissions.vue')
const ComputeMesh = () => import('../views/ComputeMesh.vue')
const CalendarSettings = () => import('../views/CalendarSettings.vue')
const Voip = () => import('../views/Voip.vue')
const PortalLogin = () => import('../views/portal/PortalLogin.vue')
const Wiki = () => import('../views/portal/Wiki.vue')
const Chat = () => import('../views/portal/Chat.vue')
const Calendar = () => import('../views/portal/Calendar.vue')
const Phone = () => import('../views/portal/Phone.vue')
const Files = () => import('../views/portal/Files.vue')
const FileShare = () => import('../views/FileShare.vue')

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: Login },
    { path: '/', name: 'dashboard', component: Dashboard },
    { path: '/modules', name: 'modules', component: ModuleStore },
    { path: '/users', name: 'users', component: Users },
    { path: '/logs', name: 'logs', component: Logs },
    { path: '/vpn', name: 'vpn', component: Vpn },
    { path: '/settings', name: 'settings', component: Settings },
    { path: '/about', name: 'about', component: About },
    { path: '/backup', name: 'backup', component: Backup },
    { path: '/wiki-permissions', name: 'wiki-permissions', component: WikiPermissions },
    { path: '/compute-mesh', name: 'compute-mesh', component: ComputeMesh },
    { path: '/calendar-settings', name: 'calendar-settings', component: CalendarSettings },
    { path: '/voip', name: 'voip', component: Voip },
    { path: '/portal/login', name: 'portal-login', component: PortalLogin },
    { path: '/portal/wiki/:pathMatch(.*)*', name: 'portal-wiki', component: Wiki },
    { path: '/portal/chat', name: 'portal-chat', component: Chat },
    { path: '/portal/calendar', name: 'portal-calendar', component: Calendar },
    { path: '/portal/phone', name: 'portal-phone', component: Phone },
    { path: '/portal/files', name: 'portal-files', component: Files },
    { path: '/file-share', name: 'file-share', component: FileShare },
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
