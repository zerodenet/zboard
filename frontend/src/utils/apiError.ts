import { normalizeOutput } from './output'
import { pluginManagementErrorMessage } from './pluginManagementErrors'
import { readPluginActionFailure } from './pluginActionError'

type ApiErrorScope = 'plugin-management'

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

export function normalizeApiErrorPayload(payload: unknown, scope?: ApiErrorScope): Record<string, any> {
  if (!payload || typeof payload !== 'object' || Array.isArray(payload)) return { message: '' }
  const source = payload as Record<string, any>
  const actionFailure = readPluginActionFailure(source)
  if (source.error?.source === 'plugin' || source.error?.code === 'plugin_action_rejected') {
    if (!actionFailure) return { message: '', error: { version: 1, code: 'unavailable', retryable: false } }
    return { code: 422, message: actionFailure.message, error: { version: 1, code: actionFailure.code, source: actionFailure.source, plugin_code: actionFailure.plugin_code, retryable: actionFailure.retryable } }
  }
  const normalized: Record<string, any> = {
    ...source,
    message: actionFailure?.message || localizedApiMessage(source.message) || (scope === 'plugin-management' ? pluginManagementErrorMessage(normalizeApiText(source.message, 500)) : ''),
  }

  if (!source.error || typeof source.error !== 'object' || Array.isArray(source.error)) {
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
  return normalized
}

export function normalizeApiErrorMessage(cause: any, fallback: string, scope?: ApiErrorScope) {
  const actionFailure = readPluginActionFailure(cause?.response?.data)
  if (actionFailure) return actionFailure.message
  const response = normalizeApiErrorPayload(cause?.response?.data, scope)
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
