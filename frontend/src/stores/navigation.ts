import { computed, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { fetchNavigation, type NavigationSnapshot } from '../api/menus'
import { useRoute } from 'vue-router'
import { NAVIGATION_CHANGED } from '../utils/navigationEvents'
import type { Surface } from '../api/plugins'
import { useAppStore } from './app'
import { toAdminNavigation } from '../utils/adminNavigation'

function state() { return { snapshot: shallowRef<NavigationSnapshot>({ revision: 0, nodes: [] }), error: ref(''), loading: ref(false), checkedPath: ref('') } }
export const navigationState = { public: state(), account: state(), admin: state() }
export const adminNavigation = computed(() => toAdminNavigation(navigationState.admin.snapshot.value.nodes))
export { NAVIGATION_CHANGED } from '../utils/navigationEvents'

// Layouts own request lifecycle; every navigation consumer reads this shared
// snapshot. Identity changes invalidate in-flight reads before clearing menus.
export function useNavigation(surface: Surface) {
  const app = useAppStore()
  const route = useRoute()
  const current = navigationState[surface]
  current.snapshot.value = { revision: 0, nodes: [] }
  current.error.value = ''
  current.checkedPath.value = ''
  const ready = computed(() => current.checkedPath.value === route.path)
  const pageAvailable = computed(() => ready.value && current.snapshot.value.page_available === true)
  const pageError = computed(() => ready.value ? '' : current.error.value)
  const pageLoading = computed(() => !ready.value && !pageError.value)
  let controller: AbortController | undefined
  let sequence = 0
  let timer: ReturnType<typeof setInterval> | undefined
  let disposed = false
  async function load() {
    if (disposed) return
    const request = ++sequence
    const path = route.path
    controller?.abort()
    controller = new AbortController()
    current.loading.value = true
    try {
      const snapshot = await fetchNavigation(surface, controller.signal, path)
      if (disposed || request !== sequence) return
      current.snapshot.value = snapshot
      current.checkedPath.value = path
      current.error.value = ''
    } catch (cause: any) {
      if (disposed || request !== sequence || controller?.signal.aborted) return
      current.error.value = cause?.response?.data?.message || '菜单暂不可用，请重试'
    } finally { if (request === sequence) current.loading.value = false }
  }
  function refresh() { if (!document.hidden) void load() }
  watch(() => app.token, () => {
    ++sequence
    controller?.abort()
    current.snapshot.value = { revision: 0, nodes: [] }
    current.error.value = ''
    current.checkedPath.value = ''
    refresh()
  })
  watch(() => route.path, () => {
    current.checkedPath.value = ''
    current.error.value = ''
    void load()
  }, { flush: 'sync' })
  onMounted(() => {
    refresh()
    timer = setInterval(refresh, 30_000)
    window.addEventListener('focus', refresh)
    window.addEventListener(NAVIGATION_CHANGED, refresh)
    document.addEventListener('visibilitychange', refresh)
  })
  onBeforeUnmount(() => {
    disposed = true
    ++sequence
    controller?.abort()
    current.loading.value = false
    if (timer) clearInterval(timer)
    window.removeEventListener('focus', refresh)
    window.removeEventListener(NAVIGATION_CHANGED, refresh)
    document.removeEventListener('visibilitychange', refresh)
  })
  return { ...current, load, pageAvailable, pageLoading, pageError }
}
