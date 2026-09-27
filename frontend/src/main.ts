import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { createRouter, createWebHistory, isNavigationFailure, NavigationFailureType, type RouteLocationNormalized } from 'vue-router'
import App from './App.vue'
import { routes } from './router'
import { useAppStore } from './stores/app'
import { AUTH_SESSION_EXPIRED_EVENT, resetAuthSessionExpired } from './utils/authSession'
import { applySiteMetadata } from './utils/siteProfile'
import { accountPurchaseRoute } from './utils/commerceNavigation'
import { finishNavigationProgress, navigationProgressGeneration, startNavigationProgress, waitForPagePaint } from './composables/navigationProgress'
import './styles.css'
import './styles/auth.css'
import './styles/public.css'
import './styles/account.css'
// Product selection and checkout use a shared responsive interaction layer.
import './styles/commerce.css'
import './styles/commerce-catalog.css'
import './styles/commerce-storefront.css'
import './theme/shadcn.css'
import './theme/design-system.css'
import './theme/admin-layout.css'
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
  routes,
  scrollBehavior(_to, _from, savedPosition) {
    return savedPosition || { top: 0 }
  },
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

window.addEventListener(AUTH_SESSION_EXPIRED_EVENT, () => {
  const store = useAppStore(pinia)
  const current = router.currentRoute.value
  store.clear()

  // Initial navigation is still covered by the route guard below. For an already
  // mounted protected route, move immediately instead of leaving a dead screen
  // that only recovers after a manual refresh.
  if (!current.meta.requiresAuth) {
    resetAuthSessionExpired()
    return
  }

  void router.replace({ path: '/login', query: { redirect: current.fullPath } })
    .finally(resetAuthSessionExpired)
})

router.beforeEach((to, from) => {
  if (from.matched.length && to.fullPath !== from.fullPath) startNavigationProgress()
})

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
	try { await store.loadSystemStatus() } catch (_) { /* API calls retain the last known state. */ }

  if (to.path === '/pricing' && store.isAuthenticated) {
    return { ...accountPurchaseRoute(to.query), replace: true }
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

router.afterEach((to, _from, failure) => {
  if (!isNavigationFailure(failure, NavigationFailureType.cancelled)) {
    const generation = navigationProgressGeneration()
    if (failure) finishNavigationProgress(generation)
    else void waitForPagePaint().then(() => finishNavigationProgress(generation))
  }
  const store = useAppStore(pinia)
  const documentSlug = String(to.params.slug || '')
  const documentTitle = to.meta.policyDocument
    ? (documentSlug
        ? store.siteProfile.policyDocuments.find(document => document.slug === documentSlug)
        : store.siteProfile.policyDocuments[0])?.title
    : ''
  applySiteMetadata(store.siteProfile, {
    path: to.path,
    pageTitle: documentTitle || (typeof to.meta.title === 'string' ? to.meta.title : ''),
  })
})
router.onError(() => finishNavigationProgress())

app.mount('#app')
