import { computed, getCurrentScope, onScopeDispose, ref, shallowRef } from 'vue'
import { createRequestGuard } from '../utils/request'

export interface CursorTablePage<
  T,
  A extends Record<string, unknown> = Record<string, unknown>,
  F extends Record<string, unknown> = Record<string, unknown>,
> {
  items: T[]
  total: number
  page: {
    next_cursor: string | null
    previous_cursor: string | null
  }
  aggregates?: A
  facets?: F
}

export interface CursorTableFetchContext {
  signal: AbortSignal
}

export interface CursorTableOptions<
  T,
  A extends Record<string, unknown> = Record<string, unknown>,
  F extends Record<string, unknown> = Record<string, unknown>,
> {
  fetchPage: (context: CursorTableFetchContext) => Promise<CursorTablePage<T, A, F>>
  errorMessage: string | ((error: unknown) => string)
  onPageLoaded?: (page: CursorTablePage<T, A, F>) => void | Promise<void>
}

export function useCursorTable<
  T,
  A extends Record<string, unknown> = Record<string, unknown>,
  F extends Record<string, unknown> = Record<string, unknown>,
>(options: CursorTableOptions<T, A, F>) {
  const items = shallowRef<T[]>([])
  const total = ref(0)
  const aggregates = shallowRef<A>({} as A)
  const facets = shallowRef<F>({} as F)
  const nextCursor = ref<string | null>(null)
  const previousCursor = ref<string | null>(null)
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
      nextCursor.value = page.page.next_cursor
      previousCursor.value = page.page.previous_cursor
      hasLoaded.value = true
      await options.onPageLoaded?.(page)
      return requests.isCurrent(request)
    } catch (cause) {
      if (!requests.isCurrent(request) || currentController.signal.aborted) return false
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
    nextCursor.value = null
    previousCursor.value = null
  }

  if (getCurrentScope()) onScopeDispose(() => { disposed = true; invalidate() })

  return { items, total, aggregates, facets, nextCursor, previousCursor, loading, initialLoading, refreshing, hasLoaded, error, load, invalidate, reset }
}
