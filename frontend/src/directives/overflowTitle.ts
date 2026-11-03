import type { ObjectDirective } from 'vue'
import { observeTextOverflow } from '../utils/textOverflow'

type Tracked = { value: unknown; overflow: boolean; measure: () => void; stop: () => void }
const tracked = new WeakMap<HTMLElement, Tracked>()
function update(element: HTMLElement) {
  const state = tracked.get(element)
  if (state?.overflow && state.value != null && String(state.value).trim()) element.setAttribute('title', String(state.value))
  else element.removeAttribute('title')
}
export const vOverflowTitle: ObjectDirective<HTMLElement, unknown> = {
  mounted(element, binding) {
    const state: Tracked = { value: binding.value, overflow: false, ...observeTextOverflow(element, overflow => { state.overflow = overflow; update(element) }) }
    tracked.set(element, state)
  },
  updated(element, binding) { const state = tracked.get(element); if (state) { state.value = binding.value; update(element); state.measure() } },
  beforeUnmount(element) { tracked.get(element)?.stop(); tracked.delete(element); element.removeAttribute('title') },
}
