export interface PluginActionFailure {
  source: 'plugin'
  code: 'plugin_action_rejected'
  plugin_code: string
  message: string
  retryable: boolean
}

// This namespace is independent of host authentication/session errors. Do not
// infer business outcomes from ordinary RPC text or legacy success JSON.
export function readPluginActionFailure(payload: unknown): PluginActionFailure | undefined {
  if (!payload || typeof payload !== 'object' || Array.isArray(payload)) return
  const value = payload as Record<string, any>, detail = value.error
  if (value.code !== 422 || detail?.version !== 1 || detail.source !== 'plugin' || detail.code !== 'plugin_action_rejected'
    || typeof detail.plugin_code !== 'string' || !/^[a-z][a-z0-9_]{0,63}$/.test(detail.plugin_code)
    || typeof detail.retryable !== 'boolean' || typeof value.message !== 'string' || !value.message
    || value.message.trim() !== value.message || new TextEncoder().encode(value.message).length > 1024
    || /[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f-\u009f]/.test(value.message)
    || Array.from(value.message as string).some(c => /^[\ud800-\udfff]$/u.test(c))) return
  return { source: 'plugin', code: 'plugin_action_rejected', plugin_code: detail.plugin_code, message: value.message, retryable: detail.retryable }
}

export function pluginActionFailure(cause: unknown) {
  return readPluginActionFailure((cause as any)?.response?.data)
}
