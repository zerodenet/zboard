import { computed, getCurrentScope, onScopeDispose, ref, shallowRef, type Ref } from 'vue'
import { createRequestGuard } from '../utils/request'

export interface RemoteTablePage<
  T,
  A extends Record<string, unknown> = Record<string, unknown>,
  F extends Record<string, unknown> = Record<string, unknown>,
> {
  items: T[]
  total: number
  aggregates?: A
  facets?: F
}

export interface RemoteTableFetchContext {
  signal: AbortSignal
}

export interface RemoteTableOptions<
  T,
  A extends Record<string, unknown> = Record<string, unknown>,
  F extends Record<string, unknown> = Record<string, unknown>,
> {
  offset: Ref<number>
  limit: Ref<number>
  fetchPage: (context: RemoteTableFetchContext) => Promise<RemoteTablePage<T, A, F>>
  errorMessage: string | ((error: unknown) => string)
  onPageLoaded?: (page: RemoteTablePage<T, A, F>) => void | Promise<void>
  onOffsetCorrected?: () => void | Promise<void>
}

export function useRemoteTable<
  T,
  A extends Record<string, unknown> = Record<string, unknown>,
  F extends Record<string, unknown> = Record<string, unknown>,
>(options: RemoteTableOptions<T, A, F>) {
  const items = shallowRef<T[]>([])
  const total = ref(0)
  const aggregates = shallowRef<A>({} as A)
  const facets = shallowRef<F>({} as F)
  const initialLoading = ref(false)
  const refreshing = ref(false)
  const hasLoaded = ref(false)
  const error = ref('')
  const requests = createRequestGuard()
  let controller: AbortController | null = null
  let disposed = false
  const loading = computed(() => initialLoading.value || refreshing.value)

  async function load(): Promise<boolean> {
    if (disposed) return false
    controller?.abort()
    controller = new AbortController()
    const currentController = controller
    const request = requests.begin()
    if (hasLoaded.value) refreshing.value = true
    else initialLoading.value = true
    error.value = ''

    try {
      const page = await options.fetchPage({ signal: currentController.signal })
      if (!requests.isCurrent(request)) return false

      items.value = page.items
      total.value = page.total
      aggregates.value = (page.aggregates || {}) as A
      facets.value = (page.facets || {}) as F
      hasLoaded.value = true

      if (options.offset.value >= page.total && options.offset.value > 0) {
        options.offset.value = Math.max(0, Math.floor(Math.max(0, page.total - 1) / options.limit.value) * options.limit.value)
        await options.onOffsetCorrected?.()
        if (!requests.isCurrent(request) || disposed) return false
        return load()
      }

      await options.onPageLoaded?.(page)
      return requests.isCurrent(request)
    } catch (cause) {
      if (!requests.isCurrent(request)) return false
      if (currentController.signal.aborted) return false
      error.value = typeof options.errorMessage === 'function' ? options.errorMessage(cause) : options.errorMessage
      return false
    } finally {
      if (requests.isCurrent(request)) {
        initialLoading.value = false
        refreshing.value = false
      }
    }
  }

  function invalidate() {
    controller?.abort()
    controller = null
    requests.invalidate()
    initialLoading.value = false
    refreshing.value = false
  }

  function reset() {
    invalidate()
    items.value = []
    total.value = 0
    aggregates.value = {} as A
    facets.value = {} as F
    hasLoaded.value = false
    error.value = ''
  }

  if (getCurrentScope()) onScopeDispose(() => { disposed = true; invalidate() })

  return { items, total, aggregates, facets, loading, initialLoading, refreshing, hasLoaded, error, load, invalidate, reset }
}
