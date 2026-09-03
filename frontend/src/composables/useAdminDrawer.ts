import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch, type Ref } from 'vue'

export function useAdminDrawer(sidebar: Ref<HTMLElement | null>) {
  const open = ref(false)
  const mobile = ref(false)
  const modal = computed(() => mobile.value && open.value)
  let media: MediaQueryList | undefined
  let previousFocus: HTMLElement | null = null
  let previousOverflow = ''
  let locked = false

  function restore() {
    if (!locked) return
    document.body.style.overflow = previousOverflow
    previousFocus?.focus()
    previousFocus = null
    locked = false
  }

  function syncViewport() {
    mobile.value = media?.matches ?? false
    if (!mobile.value) open.value = false
  }

  function onKeydown(event: KeyboardEvent) {
    if (!modal.value) return
    if (event.key === 'Escape') {
      event.preventDefault()
      open.value = false
    }
    if (event.key !== 'Tab') return
    const elements = Array.from(sidebar.value?.querySelectorAll<HTMLElement>('a[href], button:not([disabled]), [tabindex="0"]') || [])
      .filter(element => element.getClientRects().length > 0)
    const first = elements[0]
    const last = elements.at(-1)
    if (event.shiftKey && (document.activeElement === first || !sidebar.value?.contains(document.activeElement))) {
      event.preventDefault(); last?.focus()
    } else if (!event.shiftKey && (document.activeElement === last || !sidebar.value?.contains(document.activeElement))) {
      event.preventDefault(); first?.focus()
    }
  }

  watch(modal, async value => {
    if (!value) { restore(); return }
    previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    locked = true
    await nextTick()
    if (modal.value) sidebar.value?.querySelector<HTMLElement>('.sidebar-close')?.focus()
  }, { flush: 'post' })

  onMounted(() => {
    media = window.matchMedia('(max-width: 820px)')
    syncViewport()
    media.addEventListener('change', syncViewport)
    document.addEventListener('keydown', onKeydown)
  })
  onBeforeUnmount(() => {
    media?.removeEventListener('change', syncViewport)
    document.removeEventListener('keydown', onKeydown)
    restore()
  })
  return { open, mobile, modal }
}
