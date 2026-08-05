import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { createRouter, createWebHistory, type RouteLocationNormalized } from 'vue-router'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import App from './App.vue'
import { routes } from './router'
import { useAppStore } from './stores/app'
import { primeVueOptions } from './theme/primevue'
import './styles.css'
import './styles/auth.css'
import './styles/public.css'
import './styles/account.css'
import './styles/commerce.css'
import MetricCard from './components/MetricCard.vue'
import PageRefreshButton from './components/PageRefreshButton.vue'
import UiButton from './components/UiButton.vue'
import UiCheckbox from './components/UiCheckbox.vue'
import UiInput from './components/UiInput.vue'
import UiSelect from './components/UiSelect.vue'
import UiTextarea from './components/UiTextarea.vue'
import UiMetricStrip from './components/UiMetricStrip.vue'
import UiSection from './components/UiSection.vue'
import UiTabs from './components/UiTabs.vue'
import FormField from './components/FormField.vue'
import TimeBadge from './components/TimeBadge.vue'

const router = createRouter({
  history: createWebHistory(),
  routes
})

const app = createApp(App)
app.component('MetricCard', MetricCard)
app.component('PageRefreshButton', PageRefreshButton)
app.component('UiButton', UiButton)
app.component('UiCheckbox', UiCheckbox)
app.component('UiInput', UiInput)
app.component('UiSelect', UiSelect)
app.component('UiTextarea', UiTextarea)
app.component('UiMetricStrip', UiMetricStrip)
app.component('UiSection', UiSection)
app.component('UiTabs', UiTabs)
app.component('FormField', FormField)
app.component('TimeBadge', TimeBadge)
const pinia = createPinia()
app.use(PrimeVue, primeVueOptions)
app.use(ToastService)
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
