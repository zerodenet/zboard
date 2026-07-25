import type { LocationQueryRaw, RouteLocationRaw } from 'vue-router'

const adminOrigin = 'https://zboard.invalid'
const maxReturnToLength = 2048

export function normalizeAdminReturnTo(value: unknown): string {
  const candidate = Array.isArray(value) ? value[0] : value
  if (typeof candidate !== 'string') return ''
  const raw = candidate.trim()
  if (!raw || raw.length > maxReturnToLength || /[\u0000-\u001f\u007f]/.test(raw)) return ''

  try {
    const parsed = new URL(raw, adminOrigin)
    if (parsed.origin !== adminOrigin || parsed.username || parsed.password) return ''
    if (!/^\/admin(?:\/|$)/.test(parsed.pathname)) return ''
    return `${parsed.pathname}${parsed.search}`
  } catch {
    return ''
  }
}

export function preserveAdminReturnTo(value: unknown): LocationQueryRaw {
  const returnTo = normalizeAdminReturnTo(value)
  return returnTo ? { return_to: returnTo } : {}
}

export function withAdminReturnTo(
  path: string,
  currentFullPath: string,
  query: LocationQueryRaw = {},
): RouteLocationRaw {
  const returnTo = normalizeAdminReturnTo(currentFullPath)
  return {
    path,
    query: {
      ...query,
      ...(returnTo ? { return_to: returnTo } : {}),
    },
  }
}
