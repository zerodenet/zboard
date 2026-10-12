import { shallowRef } from 'vue'
import type { Router } from 'vue-router'

export const frontendLoadFailure = shallowRef<'assets' | 'navigation' | null>(null)

function isAssetLoadError(error: unknown): boolean {
  const message = error instanceof Error ? error.message : String(error)
  return /Failed to fetch dynamically imported module|Importing a module script failed|error loading dynamically imported module|Unable to preload CSS|Loading (?:CSS )?chunk .+ failed|ChunkLoadError/i.test(message)
}

export function installFrontendLoadRecovery(router: Router): () => void {
  const onPreloadError = () => { frontendLoadFailure.value = 'assets' }
  // Preserve Vite's rejection so Vue Router also aborts the failed navigation.
  window.addEventListener('vite:preloadError', onPreloadError)
  const removeError = router.onError(error => {
    frontendLoadFailure.value = isAssetLoadError(error) || frontendLoadFailure.value === 'assets' ? 'assets' : 'navigation'
  })
  const removeAfter = router.afterEach((_to, _from, failure) => {
    if (!failure) frontendLoadFailure.value = null
  })
  return () => {
    window.removeEventListener('vite:preloadError', onPreloadError)
    removeError()
    removeAfter()
  }
}

export function reloadFrontend(): void {
  // Keep the URL, login and user preferences. Never automatically reload an
  // open editor or loop when the next deployment is still unavailable.
  window.location.reload()
}
