import { computed, shallowRef, watch } from 'vue'

const hosts = shallowRef<HTMLElement[]>([])
export const activeTaskTrayHost = computed(() => hosts.value.at(-1))

// Keep one task tray and one poller. Drawer controls must participate in the
// active dialog's pointer, accessibility and keyboard-focus boundary.
export function useTaskTrayHost() {
  const host = shallowRef<HTMLElement | null>(null)
  watch(host, (element, _previous, onCleanup) => {
    if (!element) return
    hosts.value = [...hosts.value, element]
    onCleanup(() => { hosts.value = hosts.value.filter(value => value !== element) })
  }, { flush: 'post' })
  return host
}
