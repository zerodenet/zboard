import { expireAuthSession } from './authSession'
import { normalizeOutput } from './output'

export interface NormalizedApiFormError {
  code: string
  message: string
  fields: Record<string, string>
}

const stableErrorCodePattern = /^[a-z0-9_.-]{1,80}$/
const stableFieldNamePattern = /^[a-zA-Z0-9_.-]{1,80}$/

function normalizeApiText(value: unknown, maxLength: number) {
  if (typeof value !== 'string') return ''
  const normalized = normalizeOutput(value)
    .replace(/\uFFFD+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
  return Array.from(normalized).slice(0, maxLength).join('')
}

function localizedApiMessage(value: unknown) {
  const message = normalizeApiText(value, 500)
  return /[\u3400-\u9fff]/.test(message) ? message : ''
}

export function normalizeApiMessage(value: unknown, fallback: string) {
  return localizedApiMessage(value) || fallback
}

export function normalizeApiErrorPayload(payload: unknown): Record<string, any> {
  if (!payload || typeof payload !== 'object' || Array.isArray(payload)) return { message: '' }
  const source = payload as Record<string, any>
  const normalized: Record<string, any> = {
    ...source,
    message: localizedApiMessage(source.message),
  }

  if (!source.error || typeof source.error !== 'object' || Array.isArray(source.error)) {
    if (Number(source.code) === 401) expireAuthSession()
    return normalized
  }

  const detail = source.error as Record<string, any>
  const fields: Record<string, string> = {}
  if (detail.fields && typeof detail.fields === 'object' && !Array.isArray(detail.fields)) {
    for (const [name, value] of Object.entries(detail.fields)) {
      if (!stableFieldNamePattern.test(name)) continue
      const message = normalizeApiText(value, 500)
      if (message) fields[name] = message
    }
  }

  const code = typeof detail.code === 'string' && stableErrorCodePattern.test(detail.code) ? detail.code : ''
  normalized.error = {
    ...detail,
    code,
    fields,
  }
  if (Number(source.code) === 401 || code === 'unauthenticated') expireAuthSession()
  return normalized
}

export function normalizeApiErrorMessage(cause: any, fallback: string) {
  const response = normalizeApiErrorPayload(cause?.response?.data)
  return normalizeApiMessage(response.message, fallback)
}

export function normalizeApiFormError(
  cause: any,
  fallback: string,
  fieldMap: Record<string, string> = {},
): NormalizedApiFormError {
  const response = normalizeApiErrorPayload(cause?.response?.data)
  const detail = response?.error
  const versioned = detail && Number(detail.version) === 1 && typeof detail.code === 'string'
  const sourceFields = versioned && detail.fields && typeof detail.fields === 'object' ? detail.fields : {}
  const fields: Record<string, string> = {}
  const restrictFields = Object.keys(fieldMap).length > 0

  for (const [source, value] of Object.entries(sourceFields)) {
    if (typeof value !== 'string' || !value) continue
    if (!stableFieldNamePattern.test(source)) continue
    if (restrictFields && !fieldMap[source]) continue
    fields[fieldMap[source] || source] = value
  }

  return {
    code: versioned ? detail.code : '',
    message: response.message || fallback,
    fields,
  }
}
