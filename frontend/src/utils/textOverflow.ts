// Measure rendered content rather than guessing from character counts. Cleanup
// also cancels queued font/resize measurements after a row or view is removed.
export function observeTextOverflow(element: HTMLElement, changed: (overflow: boolean) => void) {
  let active = true, queued = false
  const measure = () => {
    if (!active || queued) return
    queued = true
    Promise.resolve().then(() => {
      queued = false
      if (!active) return
      const rect = element.getBoundingClientRect(), parent = element.parentElement
      let clipped = element.clientWidth > 0 && (element.scrollWidth > element.clientWidth + 1 || element.scrollHeight > element.clientHeight + 1)
      if (parent && rect.width > 0 && rect.height > 0) {
        const boundary = parent.getBoundingClientRect(), style = getComputedStyle(parent)
        clipped ||= /hidden|clip/.test(style.overflowX) && (rect.right > boundary.right + 1 || rect.left < boundary.left - 1)
        clipped ||= /hidden|clip/.test(style.overflowY) && (rect.bottom > boundary.bottom + 1 || rect.top < boundary.top - 1)
      }
      changed(clipped)
    })
  }
  const observer = typeof ResizeObserver !== 'undefined' ? new ResizeObserver(measure) : undefined
  observer?.observe(element)
  if (element.parentElement) observer?.observe(element.parentElement)
  if (!observer) window.addEventListener('resize', measure)
  document.fonts?.ready.then(measure)
  document.fonts?.addEventListener('loadingdone', measure)
  measure()
  return { measure, stop: () => { active = false; observer?.disconnect(); window.removeEventListener('resize', measure); document.fonts?.removeEventListener('loadingdone', measure) } }
}
