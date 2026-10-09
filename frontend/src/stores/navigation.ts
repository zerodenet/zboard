import { computed, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { fetchNavigation, type NavigationSnapshot } from '../api/menus'
import { useRoute } from 'vue-router'
import { NAVIGATION_CHANGED } from '../utils/navigationEvents'
import type { Surface } from '../api/plugins'
import { useAppStore } from './app'
import { normalizeApiErrorMessage } from '../utils/apiError'
import { toAdminNavigation } from '../utils/adminNavigation'

export type NavigationFailure = '' | 'session-expired' | 'forbidden' | 'network'
function state() { return { snapshot: shallowRef<NavigationSnapshot>({ revision: 0, nodes: [] }), error: ref(''), failure: ref<NavigationFailure>(''), loading: ref(false), checkedPath: ref('') } }
export const navigationState = { public: state(), account: state(), admin: state() }
export const adminNavigation = computed(() => toAdminNavigation(navigationState.admin.snapshot.value.nodes))
export { NAVIGATION_CHANGED } from '../utils/navigationEvents'
let approvedTransitionPath = ''
export function markApprovedNavigationTransition(path: string, event: MouseEvent) {
  if ((event.button != null && event.button !== 0) || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
  approvedTransitionPath = path
  window.setTimeout(() => { if (approvedTransitionPath === path) approvedTransitionPath = '' }, 5000)
}

// Layouts own request lifecycle; every navigation consumer reads this shared
// snapshot. Identity changes invalidate in-flight reads before clearing menus.
export function useNavigation(surface: Surface) {
  const app = useAppStore()
  const route = useRoute()
  const current = navigationState[surface]
  current.snapshot.value = { revision: 0, nodes: [] }
  current.error.value = ''
  current.failure.value = ''
  current.checkedPath.value = ''
  const ready = computed(() => current.checkedPath.value === route.path)
  const cachedPath = ref('')
  const pageAvailable = computed(() => ready.value && (cachedPath.value === route.path || current.snapshot.value.page_available === true))
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
      cachedPath.value = ''
      current.error.value = ''
      current.failure.value = ''
    } catch (cause: any) {
      if (disposed || request !== sequence || controller?.signal.aborted) return
      const status = Number(cause?.response?.status)
      current.failure.value = status === 401 ? 'session-expired' : status === 403 ? 'forbidden' : 'network'
      if (status === 401 || status === 403) {
        // A previous approval cannot outlive an explicit access denial.
        current.snapshot.value = { revision: 0, nodes: [] }
        current.checkedPath.value = ''
        cachedPath.value = ''
      }
      current.error.value = status === 401 ? '登录已过期，请重新登录。'
        : status === 403 ? '访问授权已失效或当前账户没有权限。'
        : normalizeApiErrorMessage(cause, '连接暂时中断，请检查网络后重试。')
    } finally { if (request === sequence) current.loading.value = false }
  }
  function refresh() { if (!document.hidden) void load() }
  watch(() => app.token, () => {
    ++sequence
    controller?.abort()
    approvedTransitionPath = ''
    current.snapshot.value = { revision: 0, nodes: [] }
    current.error.value = ''
    current.failure.value = ''
    current.checkedPath.value = ''
    cachedPath.value = ''
    refresh()
  })
  watch(() => route.path, () => {
    const path = route.path
    const approved = surface === 'admin' && approvedTransitionPath === path && current.snapshot.value.nodes.some(node => node.path === path && node.owner !== 'custom')
    approvedTransitionPath = ''
    cachedPath.value = approved ? path : ''
    current.checkedPath.value = approved ? path : ''
    current.error.value = ''
    current.failure.value = ''
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
