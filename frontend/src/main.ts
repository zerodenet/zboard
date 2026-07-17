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
    requiresGuest: Boolean(to.meta.requiresGuest)
  }
}

router.beforeEach(async (to) => {
  const store = useAppStore(pinia)
  if (store.token && !store.user.username) {
    await store.loadMe()
  }

  const meta = resolveMeta(to)
  if (meta.requiresAuth && !store.isAuthenticated) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  if (meta.requiresGuest && store.isAuthenticated) {
    return '/dashboard'
  }
  if (meta.requiresAdmin && !store.isAdmin) {
    return '/dashboard'
  }

  return true
})

app.mount('#app')
