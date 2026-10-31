import { computed, readonly, ref } from 'vue'

export type AppearancePreference = 'system' | 'light' | 'dark'
export const appearanceStorageKey = 'zboard.appearance'

function preference(value: string | null): AppearancePreference {
  return value === 'light' || value === 'dark' ? value : 'system'
}

export function createAppearance(target: Window) {
  let stored: string | null = null
  try { stored = target.localStorage.getItem(appearanceStorageKey) } catch { /* Private browsing can deny storage. */ }
  const selected = ref(preference(stored))
  const media = typeof target.matchMedia === 'function' ? target.matchMedia('(prefers-color-scheme: dark)') : undefined
  const systemDark = ref(media?.matches || false)
  const resolved = computed(() => selected.value === 'system' ? (systemDark.value ? 'dark' : 'light') : selected.value)
  function apply() {
    const root = target.document.documentElement
    root.classList.toggle('dark', resolved.value === 'dark')
    root.style.colorScheme = resolved.value
    root.dataset.appearance = selected.value
  }
  function setPreference(value: AppearancePreference) {
    selected.value = preference(value)
    try { target.localStorage.setItem(appearanceStorageKey, selected.value) } catch { /* The current page still switches. */ }
    apply()
  }
  function onSystemChange(event: MediaQueryListEvent) { systemDark.value = event.matches; apply() }
  function onStorage(event: StorageEvent) {
    if (event.key !== appearanceStorageKey && event.key !== null) return
    try { if (event.storageArea && event.storageArea !== target.localStorage) return } catch { return }
    selected.value = preference(event.newValue)
    apply()
  }
  if (media?.addEventListener) media.addEventListener('change', onSystemChange)
  else media?.addListener?.(onSystemChange)
  target.addEventListener('storage', onStorage)
  apply()
  return {
    preference: readonly(selected), resolved, setPreference,
    dispose() {
      if (media?.removeEventListener) media.removeEventListener('change', onSystemChange)
      else media?.removeListener?.(onSystemChange)
      target.removeEventListener('storage', onStorage)
    },
  }
}

let appearance: ReturnType<typeof createAppearance> | undefined
export function useAppearance() { return appearance ||= createAppearance(window) }
