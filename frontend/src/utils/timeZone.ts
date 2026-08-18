import { ref } from 'vue'

export const DEFAULT_SYSTEM_TIME_ZONE = 'UTC'

const activeTimeZone = ref(DEFAULT_SYSTEM_TIME_ZONE)

export function isValidTimeZone(value: unknown): value is string {
  if (typeof value !== 'string' || !value.trim()) return false
  try {
    new Intl.DateTimeFormat('en-US', { timeZone: value.trim() }).format(new Date(0))
    return true
  } catch {
    return false
  }
}

export function setDisplayTimeZone(value: unknown) {
  activeTimeZone.value = isValidTimeZone(value) ? value.trim() : DEFAULT_SYSTEM_TIME_ZONE
  return activeTimeZone.value
}

export function getDisplayTimeZone() {
  return activeTimeZone.value
}
