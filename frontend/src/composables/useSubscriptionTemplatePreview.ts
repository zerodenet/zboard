import { computed, onBeforeUnmount, ref, watch, type ComputedRef } from 'vue'
import { previewSubscriptionTemplate, type SubscriptionTemplatePreview, type SubscriptionTemplatePreviewRequest } from '../api/client'
import { useRemoteResource } from './useRemoteResource'

export function useSubscriptionTemplatePreview(options: {
  enabled: ComputedRef<boolean>
  identity: ComputedRef<string>
  request: ComputedRef<SubscriptionTemplatePreviewRequest>
  invalid: ComputedRef<string>
}) {
  const source = computed(() => JSON.stringify(options.request.value))
  const identityBlocked = ref(false)
  const resource = useRemoteResource<{ source: string; preview: SubscriptionTemplatePreview | null }>({
    initial: () => ({ source: '', preview: null }),
    fetch: async ({ signal }) => {
      if (identityBlocked.value || !options.enabled.value || options.invalid.value) throw Object.assign(new Error('preview identity unavailable'), { code: 'AUTH_SESSION_CHANGED' })
      const snapshot = source.value, identity = options.identity.value
      const preview = await previewSubscriptionTemplate(JSON.parse(snapshot), { signal })
      if (signal.aborted || !options.enabled.value || identity !== options.identity.value || snapshot !== source.value) throw Object.assign(new Error('preview scope changed'), { code: 'AUTH_SESSION_CHANGED' })
      if (!preview || typeof preview.content !== 'string') throw new Error('预览结果不完整，请重试。')
      return { source: snapshot, preview }
    },
    errorMessage: (cause: any) => cause?.response?.data?.error?.fields?.customization || cause?.response?.data?.message || '预览暂时无法更新，请重试。',
  })
  let timer: ReturnType<typeof setTimeout> | undefined
  async function refresh() {
    clearTimeout(timer)
    if (!options.enabled.value || options.invalid.value || identityBlocked.value) return
    await resource.load()
  }
  watch([options.enabled, options.identity, source, options.invalid], ([enabled, identity], previous) => {
    clearTimeout(timer)
    resource.invalidate()
    if (previous && identity !== previous[1]) identityBlocked.value = false
    if (!enabled || previous && identity !== previous[1]) resource.reset()
    resource.error.value = ''
    if (enabled && !options.invalid.value) timer = setTimeout(() => void refresh(), 450)
  }, { immediate: true, flush: 'sync' })
  function storageChanged(event: StorageEvent) {
    if (event.key !== null && event.key !== 'zboard.auth.token') return
    clearTimeout(timer); identityBlocked.value = true; resource.reset()
  }
  window.addEventListener('storage', storageChanged)
  onBeforeUnmount(() => { clearTimeout(timer); window.removeEventListener('storage', storageChanged) })
  return {
    result: computed(() => resource.data.value.preview),
    loading: resource.loading,
    error: computed(() => identityBlocked.value ? '登录状态已变化，请刷新页面后重试。' : options.invalid.value || resource.error.value),
    stale: computed(() => Boolean(resource.data.value.preview) && (resource.data.value.source !== source.value || Boolean(options.invalid.value) || Boolean(resource.error.value))),
    refresh,
  }
}
