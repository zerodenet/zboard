import { reactive } from 'vue'
import { fetchPluginComposition, type CatalogSlot, type Surface } from '../api/plugins'
import { getAuthToken } from '../api/client'
import { NAVIGATION_CHANGED } from '../utils/navigationEvents'

interface CompositionState { slots: CatalogSlot[]; loading: boolean; loaded: boolean; error: string; revision: string; updatedAt: number }
interface Entry { state: CompositionState; token: string; surface: Surface; generation: number; controller?: AbortController; promise?: Promise<void> }
const entries = new Map<string, Entry>()
export function compositionState(surface: Surface, token = getAuthToken()) {
  const credential = surface === 'public' ? '' : token
  // Private presentation is memory-only and follows the exact authenticated identity.
  for (const [key, entry] of entries) if (entry.surface !== 'public' && entry.token !== credential && surface !== 'public' && credential === getAuthToken()) { entry.generation++; entry.controller?.abort(); entry.state.slots = []; entries.delete(key) }
  const key = `${surface}:${credential}`
  if (!entries.has(key)) entries.set(key, { surface, token: credential, generation: 0, state: reactive({ slots: [], loading: false, loaded: false, error: '', revision: '', updatedAt: 0 }) })
  return entries.get(key)!
}
export async function preparePluginComposition(surface: Surface, force = false, token = getAuthToken()): Promise<void> {
  const entry = compositionState(surface, token)
  if (surface !== 'public' && entry.token !== getAuthToken()) {
    entry.generation++; entry.controller?.abort(); entry.promise = undefined
    entry.state.slots = []; entry.state.loaded = false; entry.state.loading = false
    entry.state.error = '登录状态已变化，请刷新页面。'; entry.state.updatedAt = Date.now(); return
  }
  if (entry.promise && !force) return entry.promise
  if (entry.promise) { entry.generation++; entry.controller?.abort(); entry.promise = undefined }
  if (!force && entry.state.updatedAt && Date.now() - entry.state.updatedAt < 30_000) return
  const requestToken = getAuthToken()
  const generation = ++entry.generation
  entry.controller = new AbortController(); entry.state.loading = true
  const controller = entry.controller
  entry.promise = (async () => {
    try {
      const value = await fetchPluginComposition(surface, controller.signal)
      if (generation !== entry.generation || controller.signal.aborted || surface !== 'public' && requestToken !== getAuthToken()) return
      if (!value || !Array.isArray(value.slots) || typeof value.revision !== 'string' || value.slots.some(slot => slot.slot.surface !== surface)) throw new Error('invalid composition')
      entry.state.slots = value.slots; entry.state.revision = value.revision
      entry.state.loaded = true; entry.state.error = ''; entry.state.updatedAt = Date.now()
    } catch (cause: any) {
      if (generation !== entry.generation || controller.signal.aborted || surface !== 'public' && requestToken !== getAuthToken()) return
      if ([401,403].includes(cause?.response?.status)) { entry.state.slots = []; entry.state.loaded = false }
      entry.state.error = '扩展内容暂时无法读取，请重试。'; entry.state.updatedAt = Date.now()
    } finally { if (generation === entry.generation) { entry.state.loading = false; entry.promise = undefined } }
  })()
  return entry.promise
}
export async function compositionSlots(surface: Surface, name: string, signal?: AbortSignal, force = false): Promise<CatalogSlot[]> {
  await preparePluginComposition(surface,force)
  if (signal?.aborted) throw new DOMException('cancelled','AbortError')
  const state = compositionState(surface).state
  if (state.error) throw new Error(state.error)
  return state.slots.filter(item => item.slot.slot === name)
}
export function clearPluginComposition() {
  for (const entry of entries.values()) { entry.generation++; entry.controller?.abort(); entry.state.slots = [] }
  entries.clear()
}
if (typeof window !== 'undefined') window.addEventListener(NAVIGATION_CHANGED, () => {
  for (const entry of [...entries.values()]) { entry.state.updatedAt = 0; void preparePluginComposition(entry.surface, true, entry.token) }
})

if (typeof window !== 'undefined') window.addEventListener('zboard:plugin-ui-identity-changed', () => {
  for (const [key,entry] of entries) if (entry.surface !== 'public') {
    entry.generation++; entry.controller?.abort(); entry.state.slots = []; entry.state.loaded = false; entry.state.loading = false; entries.delete(key)
  }
})

if (typeof window !== 'undefined') window.addEventListener('storage', event => {
  if (event.key !== 'zboard.auth.token' && event.key !== null) return
  for (const [key,entry] of entries) if (entry.surface !== 'public') {
    entry.generation++;entry.controller?.abort();entry.state.slots=[];entry.state.loaded=false;entry.state.loading=false;entry.state.error='登录状态已变化，请刷新页面。';entries.delete(key)
  }
})
