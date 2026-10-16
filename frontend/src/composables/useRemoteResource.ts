import { computed, getCurrentScope, onScopeDispose, ref, shallowRef } from 'vue'
import { createRequestGuard } from '../utils/request'

export type ReadPhase = 'idle' | 'loading' | 'ready' | 'refreshing' | 'stale' | 'error' | 'unauthorized' | 'forbidden'
export type ReadErrorKind = '' | 'timeout' | 'network' | 'unauthorized' | 'forbidden' | 'server' | 'unknown'

/** Module-owned data, shared read lifecycle. Only the latest mounted read commits. */
export function useRemoteResource<T>(options: {
  initial: () => T
  fetch: (context: { signal: AbortSignal }) => Promise<T>
  errorMessage: string | ((cause: unknown) => string)
  timeoutMs?: number
}) {
  const data = shallowRef<T>(options.initial())
  const loading = ref(false)
  const loaded = ref(false)
  const error = ref('')
  const errorKind = ref<ReadErrorKind>('')
  const failureCount = ref(0)
  const canAutoRefresh = computed(() => failureCount.value < 3 && !['unauthorized', 'forbidden'].includes(errorKind.value))
  const lastSuccessAt = ref<string | null>(null)
  const refreshing = computed(() => loading.value && loaded.value)
  const stale = computed(() => loaded.value && Boolean(error.value))
  const phase = computed<ReadPhase>(() => {
    if (loading.value) return loaded.value ? 'refreshing' : 'loading'
    if (errorKind.value === 'unauthorized' || errorKind.value === 'forbidden') return errorKind.value
    if (error.value) return loaded.value ? 'stale' : 'error'
    return loaded.value ? 'ready' : 'idle'
  })
  const requests = createRequestGuard()
  let controller: AbortController | null = null
  let disposed = false

  function invalidate() {
    controller?.abort()
    controller = null
    requests.invalidate()
    loading.value = false
  }

  function reset() {
    invalidate()
    data.value = options.initial()
    loaded.value = false
    error.value = ''
    errorKind.value = ''
    failureCount.value = 0
    lastSuccessAt.value = null
  }

  function replace(value: T) {
    if (disposed) return
    invalidate()
    data.value = value
    loaded.value = true
    error.value = ''
    errorKind.value = ''
    failureCount.value = 0
    lastSuccessAt.value = new Date().toISOString()
  }

  async function load(): Promise<boolean> {
    if (disposed) return false
    invalidate()
    const current = new AbortController()
    controller = current
    const request = requests.begin()
    loading.value = true
    error.value = ''
    errorKind.value = ''
    let timedOut = false
    // Settle even if a transport ignores AbortSignal. Cancellation never commits
    // an error to a newer scope or a disposed component.
    let abort!: () => void
    const cancelled = new Promise<never>((_, reject) => {
      abort = () => reject(new Error('read cancelled'))
      current.signal.addEventListener('abort', abort, { once: true })
    })
    const timer = setTimeout(() => { timedOut = true; current.abort() }, options.timeoutMs ?? 30_000)
    try {
      const result = await Promise.race([options.fetch({ signal: current.signal }), cancelled])
      if (!requests.isCurrent(request)) return false
      data.value = result
      loaded.value = true
      failureCount.value = 0
      lastSuccessAt.value = new Date().toISOString()
      return true
    } catch (cause: any) {
      if (!requests.isCurrent(request) || (current.signal.aborted && !timedOut)) return false
      failureCount.value++
      const status = cause?.response?.status
      errorKind.value = status === 401 ? 'unauthorized' : status === 403 ? 'forbidden'
        : timedOut || status === 408 || status === 504 || ['ECONNABORTED', 'ETIMEDOUT'].includes(cause?.code) ? 'timeout'
        : cause?.code === 'ERR_NETWORK' ? 'network' : status >= 500 ? 'server' : 'unknown'
      if (status === 401 || status === 403) {
        data.value = options.initial()
        loaded.value = false
        lastSuccessAt.value = null
      }
      error.value = cause?.response?.data?.message || (typeof options.errorMessage === 'function' ? options.errorMessage(cause) : options.errorMessage)
      return false
    } finally {
      clearTimeout(timer)
      current.signal.removeEventListener('abort', abort)
      if (requests.isCurrent(request)) {
        controller = null
        loading.value = false
      }
    }
  }

  if (getCurrentScope()) onScopeDispose(() => { disposed = true; invalidate() })

  return { data, loading, loaded, error, errorKind, phase, refreshing, stale, failureCount, canAutoRefresh, lastSuccessAt, load, reset, replace, invalidate }
}
