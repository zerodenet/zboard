import { nextTick, ref } from 'vue'

export const navigationProgress = ref(0)
export const navigationProgressVisible = ref(false)

let advanceTimer: ReturnType<typeof setInterval> | undefined
let finishTimer: ReturnType<typeof setTimeout> | undefined
let hideTimer: ReturnType<typeof setTimeout> | undefined
let generation = 0
let startedAt = 0

const MIN_VISIBLE_MS = 480

function clearTimers() {
  if (advanceTimer) clearInterval(advanceTimer)
  if (finishTimer) clearTimeout(finishTimer)
  if (hideTimer) clearTimeout(hideTimer)
  advanceTimer = undefined
  finishTimer = undefined
  hideTimer = undefined
}

export function startNavigationProgress() {
  generation++
  startedAt = Date.now()
  if (finishTimer) clearTimeout(finishTimer)
  if (hideTimer) clearTimeout(hideTimer)
  finishTimer = undefined
  hideTimer = undefined
  if (navigationProgressVisible.value && advanceTimer) return
  clearTimers()
  navigationProgress.value = 8
  navigationProgressVisible.value = true
  advanceTimer = setInterval(() => {
    navigationProgress.value = Math.min(90, navigationProgress.value + Math.max(2, (90 - navigationProgress.value) * .22))
  }, 180)
}

export function navigationProgressGeneration() { return generation }

export function finishNavigationProgress(expectedGeneration = generation) {
  if (!navigationProgressVisible.value || expectedGeneration !== generation) return
  if (finishTimer) clearTimeout(finishTimer)
  finishTimer = setTimeout(() => {
    if (expectedGeneration !== generation) return
    if (advanceTimer) clearInterval(advanceTimer)
    advanceTimer = undefined
    finishTimer = undefined
    navigationProgress.value = 100
    hideTimer = setTimeout(() => {
      if (expectedGeneration !== generation) return
      navigationProgressVisible.value = false
      navigationProgress.value = 0
      hideTimer = undefined
    }, 220)
  }, Math.max(0, MIN_VISIBLE_MS - (Date.now() - startedAt)))
}

export async function waitForPagePaint() {
  await nextTick()
  await new Promise<void>(resolve => {
    let settled = false
    const finish = () => {
      if (settled) return
      settled = true
      clearTimeout(fallback)
      resolve()
    }
    const fallback = setTimeout(finish, 250)
    requestAnimationFrame(() => requestAnimationFrame(finish))
  })
}
