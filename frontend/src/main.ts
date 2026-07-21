import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { createRouter, createWebHistory, type RouteLocationNormalized } from 'vue-router'
import App from './App.vue'
import { routes } from './router'
import { useAppStore } from './stores/app'
import './styles.css'

const router = createRouter({
  history: createWebHistory(),
  routes
})

const app = createApp(App)
const pinia = createPinia()
app.use(pinia)
app.use(router)

const resolveMeta = (to: RouteLocationNormalized) => {
  return {
    requiresAuth: Boolean(to.meta.requiresAuth),
    requiresAdmin: Boolean(to.meta.requiresAdmin),
	requiresGuest: Boolean(to.meta.requiresGuest),
	requiresRegistration: Boolean(to.meta.requiresRegistration)
  }
}

const roleLanding = (store: ReturnType<typeof useAppStore>) => store.isAdmin ? '/admin/dashboard' : '/account'

router.beforeEach(async (to) => {
  const store = useAppStore(pinia)
  try {
    await store.loadSetupStatus()
  } catch (_) {
    // Keep the requested route visible; API calls will surface connectivity errors.
    return true
  }
  if (!store.isInstalled && to.path !== '/setup') {
    return '/setup'
  }
  if (store.isInstalled && to.meta.setupOnly) {
    return store.isAuthenticated ? roleLanding(store) : '/login'
  }
  if (store.token && !store.user.email) {
    await store.loadMe()
  }

  const meta = resolveMeta(to)
  if (meta.requiresAuth && !store.isAuthenticated) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  if (meta.requiresGuest && store.isAuthenticated) {
	return roleLanding(store)
  }
  if (meta.requiresAdmin && !store.isAdmin) {
	return '/account'
  }
	if (meta.requiresRegistration && !store.installation?.allow_registration) return '/login'

  return true
})

app.mount('#app')
