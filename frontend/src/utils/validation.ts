export type ValidationMessage = string | null | undefined | false

const emailPattern = /^[^\s@]+@[^\s@]+\.[^\s@]+$/
const slugPattern = /^[a-z0-9]+(?:-[a-z0-9]+)*$/

export function utf8ByteLength(value: unknown): number {
  return new TextEncoder().encode(String(value ?? '')).length
}

export function unicodeCharacterLength(value: unknown): number {
  return Array.from(String(value ?? '')).length
}

export function isBlank(value: unknown): boolean {
  return typeof value !== 'string' || value.trim() === ''
}

export function isEmail(value: unknown, maxBytes = 128): boolean {
  if (typeof value !== 'string') return false
  const normalized = value.trim()
  return normalized !== '' && utf8ByteLength(normalized) <= maxBytes && emailPattern.test(normalized)
}

export function isSlug(value: unknown, maxBytes = Number.POSITIVE_INFINITY): boolean {
  if (typeof value !== 'string') return false
  const normalized = value.trim()
  return normalized !== '' && utf8ByteLength(normalized) <= maxBytes && slugPattern.test(normalized)
}

export function isHttpUrl(value: unknown): boolean {
  if (typeof value !== 'string' || value.trim() === '') return false
  try {
    const parsed = new URL(value.trim())
    return (parsed.protocol === 'http:' || parsed.protocol === 'https:') && parsed.hostname !== '' && parsed.username === '' && parsed.password === '' && parsed.hash === ''
  } catch {
    return false
  }
}

export function isOneOf<T extends string | number>(value: unknown, allowed: readonly T[]): value is T {
  return allowed.some(candidate => candidate === value)
}

export function isIntegerInRange(value: unknown, min: number, max: number): boolean {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= min && value <= max
}

export function isUtf8LengthInRange(value: unknown, min: number, max: number, trim = false): boolean {
  const text = typeof value === 'string' ? (trim ? value.trim() : value) : ''
  const length = utf8ByteLength(text)
  return length >= min && length <= max
}

export function isCharacterLengthInRange(value: unknown, min: number, max: number, trim = false): boolean {
  const text = typeof value === 'string' ? (trim ? value.trim() : value) : ''
  const length = unicodeCharacterLength(text)
  return length >= min && length <= max
}

export function collectFieldErrors(rules: Record<string, ValidationMessage>): Record<string, string> {
  return Object.fromEntries(
    Object.entries(rules).filter((entry): entry is [string, string] => typeof entry[1] === 'string' && entry[1] !== ''),
  )
}
