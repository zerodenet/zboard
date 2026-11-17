import { describe, expect, it } from 'vitest'
import { readPluginActionFailure } from './pluginActionError'
import { normalizeApiErrorMessage, normalizeApiErrorPayload } from './apiError'

const rejected = (changes = {}) => ({ code: 422, message: 'Resource is busy. <b>Try later</b>', error: { version: 1, source: 'plugin', code: 'plugin_action_rejected', plugin_code: 'resource_busy', retryable: true }, ...changes })
describe('public plugin action failures', () => {
  it('preserves public text, namespaced codes and retry flags through normalization', () => {
    const failure = readPluginActionFailure(rejected())!
    expect(failure.plugin_code).toBe('resource_busy')
    expect(failure.retryable).toBe(true)
    expect(normalizeApiErrorPayload(rejected()).message).toBe(failure.message)
    expect(normalizeApiErrorMessage({ response: { data: rejected() } }, 'fallback')).toBe(failure.message)
  })
  it.each([
    rejected({ message: 'x'.repeat(1025) }), rejected({ message: 'private\0stack' }),
    rejected({ message: '\ud800' }), rejected({ error: { version: 1, source: 'plugin', code: 'plugin_action_rejected', plugin_code: 'forged host', retryable: true } }),
    rejected({ error: { version: 1, source: 'plugin', code: 'permission_denied', plugin_code: 'busy', retryable: true } }),
    rejected({ error: { version: 1, source: 'plugin', code: 'plugin_action_rejected', plugin_code: 'busy', retryable: 'true' } }),
  ])('safely downgrades malformed public data without exposing its message', payload => {
    expect(readPluginActionFailure(payload)).toBeUndefined()
    expect(normalizeApiErrorPayload(payload).message).toBe('')
    expect(normalizeApiErrorPayload(payload).error.code).toBe('unavailable')
  })
  it('does not reinterpret legacy success JSON or host authentication errors', () => {
    expect(readPluginActionFailure({ ok: false, error: 'legacy JSON data' })).toBeUndefined()
    expect(readPluginActionFailure({ code: 403, message: 'host', error: { version: 1, code: 'permission_denied' } })).toBeUndefined()
  })
})
