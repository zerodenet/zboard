import { getCurrentScope, onScopeDispose, ref, shallowRef } from 'vue'
import { createRequestGuard } from '../utils/request'

/** Independent read state: only the latest, still-mounted request may commit. */
export function useRemoteResource<T>(options: {
  initial: () => T
  fetch: (context: { signal: AbortSignal }) => Promise<T>
  errorMessage: string
}) {
  const data = shallowRef<T>(options.initial())
  const loading = ref(false)
  const loaded = ref(false)
  const error = ref('')
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
  }

  function replace(value: T) {
    if (disposed) return
    invalidate()
    data.value = value
    loaded.value = true
    error.value = ''
  }

  async function load(): Promise<boolean> {
    if (disposed) return false
    invalidate()
    const current = new AbortController()
    controller = current
    const request = requests.begin()
    loading.value = true
    error.value = ''
    try {
      const result = await options.fetch({ signal: current.signal })
      if (!requests.isCurrent(request)) return false
      data.value = result
      loaded.value = true
      return true
    } catch (cause: any) {
      if (!requests.isCurrent(request) || current.signal.aborted) return false
      error.value = cause?.response?.data?.message || options.errorMessage
      return false
    } finally {
      if (requests.isCurrent(request)) {
        controller = null
        loading.value = false
      }
    }
  }

  if (getCurrentScope()) onScopeDispose(() => { disposed = true; invalidate() })

  return { data, loading, loaded, error, load, reset, replace, invalidate }
}
