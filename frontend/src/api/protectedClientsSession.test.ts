import { AxiosError, type AxiosAdapter, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { authenticatedAPI, clearAuthToken, getAuthToken, setAuthToken } from './client'
import { fetchOrderSubscriptionAccess, fetchSubscriptionAccess, revokeSubscriptionAccess, rotateSubscriptionAccess } from './subscriptionAccess'
import { createManagedRuleSet, deleteManagedRuleSet, fetchManagedRuleSet, fetchManagedRuleSetContent, fetchManagedRuleSetsPage, importManagedRuleSet, updateManagedRuleSet, updateManagedRuleSetContent, type ManagedRuleSetWriteRequest } from './managedRuleSets'
import { fetchAdminEntityReferences, fetchTrafficTrends } from './readModels'
import { evaluateSubscriptionFairUse, fetchFairUseMetrics, fetchFairUsePolicy, updateSubscriptionFairUsePolicy, type FairUsePolicy } from './fairUse'
import { fetchTrafficNodeSeries, fetchTrafficUsagePage } from './trafficUsage'
import { fetchAdminSystemInfo } from './system'
import { AUTH_SESSION_EXPIRED_EVENT, isAuthSessionExpired, resetAuthSessionExpired } from '../utils/authSession'

const originalAdapter = authenticatedAPI.defaults.adapter
const request = { name: 'Rules', description: '', tag: 'rules', source_format: 'domain_list', content: 'example.test', sync_interval: 60, is_active: true } satisfies ManagedRuleSetWriteRequest
const policy: FairUsePolicy = { scope_type: 'subscription', scope_id: 3, enabled: true, evaluation_interval_seconds: 60, connection_start_threshold: 100, connection_start_window_seconds: 60, connection_start_penalty: 1, working_node_threshold: 4, working_node_window_seconds: 60, working_node_penalty: 2, score_max: 100, recovery_per_interval: 1, warning_score: 20, violation_score: 80, enforcement_mode: 'observe', restriction_duration_seconds: 60, revision: 4 }
const dateQuery = { from: '2026-10-01', to: '2026-10-02' }
const access = { configured: true, subscription_id: 3, subscription_url: 'https://fixture.invalid/subscription/private' }
const page = { items: [{ id: 7, revision: 2 }], total: 1, offset: 0, limit: 5 }
const trends = { projection_pending: true, scope: { administrative: false, user_id: 9, subscription_id: 0, node_id: 0, protocol_endpoint_id: 0, time_zone: 'UTC' }, ...dateQuery, points: null, record_count: null, connection_sample_count: null, peak_connections: null, truncated: null, subscriptions: null, as_of: null }
const response = (config: InternalAxiosRequestConfig, data: unknown) => ({ status: 200, statusText: 'OK', headers: {}, config, data: { data } })
function reject(status: number, config: InternalAxiosRequestConfig, data: unknown = { message: '请求被拒绝' }): never {
  throw new AxiosError('rejected', 'ERR_BAD_RESPONSE', config, undefined, { status, statusText: 'Rejected', headers: {}, config, data })
}
const usage = { items: [], page: { total: null }, total: null, aggregates: null }
const fairness = { subscription_id: 3, current_active_flows: null, coverage: { state: 'unknown' } }
const system = { service: 'ZBoard', version: 'test-only' }
const clients = [
  { name: 'subscription access', invoke: () => fetchSubscriptionAccess(3), body: access, timeout: 8000, path: '/account/subscriptions/3/access' },
  { name: 'managed rule sets', invoke: () => fetchManagedRuleSetsPage({ offset: 0, limit: 5 }), body: page, timeout: 30000, path: '/admin/subscription-rule-sets' },
  { name: 'read models', invoke: () => fetchTrafficTrends(dateQuery), body: trends, timeout: 8000, path: '/traffic/trends' },
  { name: 'fair use', invoke: () => fetchFairUseMetrics(3), body: fairness, timeout: 30000, path: '/admin/subscriptions/3/fair-use/metrics' },
  { name: 'traffic usage', invoke: () => fetchTrafficUsagePage({ includeTotals: false }), body: usage, timeout: 30000, path: '/traffic/records' },
  { name: 'admin system info', invoke: () => fetchAdminSystemInfo(), body: system, timeout: 30000, path: '/admin/system-info' },
]
beforeEach(() => {
  clearAuthToken(); resetAuthSessionExpired(); setAuthToken('current')
  authenticatedAPI.defaults.adapter = async config => response(config, {})
})
afterEach(() => {
  authenticatedAPI.defaults.adapter = originalAdapter
  clearAuthToken(); resetAuthSessionExpired()
  vi.restoreAllMocks()
})

describe.each(clients)('$name shared authentication boundary', client => {
  it('expires the session once using only this client, even for concurrent HTTP 401 failures', async () => {
    const expired = vi.fn()
    window.addEventListener(AUTH_SESSION_EXPIRED_EVENT, expired)
    authenticatedAPI.defaults.adapter = async config => reject(401, config)
    try {
      const outcomes = await Promise.allSettled([client.invoke(), client.invoke()])
      expect(outcomes.every(outcome => outcome.status === 'rejected')).toBe(true)
      expect(isAuthSessionExpired()).toBe(true)
      expect(expired).toHaveBeenCalledOnce()
    } finally { window.removeEventListener(AUTH_SESSION_EXPIRED_EVENT, expired) }
  })
  it('discards a successful old-account response without exposing its body or expiring the replacement login', async () => {
    setAuthToken('previous-account')
    authenticatedAPI.defaults.adapter = async config => {
      expect(config.headers.Authorization).toBe('Bearer previous-account')
      setAuthToken('replacement-account')
      return response(config, client.body)
    }
    await expect(client.invoke()).rejects.toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
    expect(getAuthToken()).toBe('replacement-account')
    expect(isAuthSessionExpired()).toBe(false)
  })
  it('does not invalidate a replacement login when the old request eventually returns 401', async () => {
    setAuthToken('previous-account')
    authenticatedAPI.defaults.adapter = async config => {
      setAuthToken('replacement-account')
      return reject(401, config)
    }
    await expect(client.invoke()).rejects.toMatchObject({ response: { status: 401 } })
    expect(getAuthToken()).toBe('replacement-account')
    expect(isAuthSessionExpired()).toBe(false)
  })
  it.each([
    [403, { message: '权限不足', error: { version: 1, code: 'forbidden' } }],
    [403, { message: '插件准入权限不足', error: { version: 1, code: 'permission_denied' } }],
    [422, { message: '插件授权失效', error: { version: 1, source: 'plugin', code: 'plugin_action_rejected', plugin_code: 'permission_denied', retryable: false } }],
  ] as const)('preserves login for permission/plugin rejection HTTP %s instead of treating it as login expiry', async (status, data) => {
    authenticatedAPI.defaults.adapter = async config => reject(status, config, data)
    await expect(client.invoke()).rejects.toMatchObject({ response: { status } })
    expect(isAuthSessionExpired()).toBe(false)
    expect(getAuthToken()).toBe('current')
  })
  it('preserves login and the rejected network failure without manufacturing an empty response', async () => {
    authenticatedAPI.defaults.adapter = async config => { throw new AxiosError('offline', 'ERR_NETWORK', config) }
    await expect(client.invoke()).rejects.toMatchObject({ code: 'ERR_NETWORK' })
    expect(isAuthSessionExpired()).toBe(false)
  })
  it('uses the original timeout and returns the original response contract through the actual Axios adapter', async () => {
    const transport = vi.fn<AxiosAdapter>(async config => {
      expect(config.timeout).toBe(client.timeout)
      expect(config.headers.Authorization).toBe('Bearer current')
      expect(config.url).toContain(client.path)
      return response(config, client.body)
    })
    authenticatedAPI.defaults.adapter = transport
    const result = await client.invoke()
    if (client.name === 'managed rule sets') expect(result).toMatchObject({ items: page.items, total: 1 })
    else if (client.name === 'traffic usage') expect(result).toMatchObject({ items: [], total: null, page: { total: null }, aggregates: null })
    else expect(result).toEqual(client.body)
    expect(transport).toHaveBeenCalledOnce()
  })
})

it('blocks all credential/rule write transports during reauthentication and never replays them after login', async () => {
  authenticatedAPI.defaults.adapter = async config => reject(401, config)
  await fetchAdminEntityReferences({ nodeIds: [1] }).catch(() => undefined)
  const transport = vi.fn<AxiosAdapter>(async config => response(config, access))
  authenticatedAPI.defaults.adapter = transport
  const writes = [
    rotateSubscriptionAccess(3), revokeSubscriptionAccess(3), fetchOrderSubscriptionAccess(10),
    createManagedRuleSet(request), updateManagedRuleSet(7, { ...request, expected_revision: 2 }), deleteManagedRuleSet(7),
    updateManagedRuleSetContent(7, 'new.example.test', 2), importManagedRuleSet(7, 'https://rules.invalid/source', 'domain_list', 2),
    updateSubscriptionFairUsePolicy(3, policy, false), evaluateSubscriptionFairUse(3),
  ]
  const outcomes = await Promise.allSettled(writes)
  expect(outcomes.every(outcome => outcome.status === 'rejected' && outcome.reason?.code === 'AUTH_SESSION_EXPIRED')).toBe(true)
  expect(transport).not.toHaveBeenCalled()
  setAuthToken('replacement')
  await Promise.resolve(); await Promise.resolve()
  expect(transport).not.toHaveBeenCalled()
  await rotateSubscriptionAccess(3)
  expect(transport).toHaveBeenCalledOnce()
})

it('also pauses every protected read without sending a transport while authentication is pending', async () => {
  authenticatedAPI.defaults.adapter = async config => reject(401, config)
  await fetchSubscriptionAccess(3).catch(() => undefined)
  const transport = vi.fn<AxiosAdapter>(async config => response(config, {}))
  authenticatedAPI.defaults.adapter = transport
  for (const client of clients) await expect(client.invoke()).rejects.toMatchObject({ code: 'AUTH_SESSION_EXPIRED' })
  expect(transport).not.toHaveBeenCalled()
})

it('retains timeout and request shape for every subscription credential operation and admin access read', async () => {
  const seen: InternalAxiosRequestConfig[] = []
  authenticatedAPI.defaults.adapter = async config => { seen.push(config); return response(config, access) }
  const signal = new AbortController().signal
  await fetchSubscriptionAccess(3, { signal })
  await rotateSubscriptionAccess(3)
  await revokeSubscriptionAccess(3)
  await fetchOrderSubscriptionAccess(10, { signal })
  expect(seen.map(config => [config.method, config.url, config.timeout])).toEqual([
    ['get', '/account/subscriptions/3/access', 8000], ['post', '/account/subscriptions/3/access/rotate', 8000],
    ['delete', '/account/subscriptions/3/access', 8000], ['post', '/admin/orders/10/subscription-access', 8000],
  ])
  expect(seen[0].signal).toBe(signal); expect(seen[3].signal).toBe(signal)
})

it('retains bounded references queries, abort signals, raw trend pending nulls and rule request bodies', async () => {
  const seen: InternalAxiosRequestConfig[] = []
  authenticatedAPI.defaults.adapter = async config => { seen.push(config); return response(config, config.url?.includes('/traffic/trends') ? trends : page) }
  const signal = new AbortController().signal
  await fetchAdminEntityReferences({ nodeIds: [4, 4, -1], targets: [' node:4 ', 'node:4'], signal })
  expect(new URL(seen[0].url!, 'https://fixture.invalid').searchParams.get('node_ids')).toBe('4')
  expect(new URL(seen[0].url!, 'https://fixture.invalid').searchParams.getAll('targets')).toEqual(['node:4'])
  expect(seen[0].signal).toBe(signal); expect(seen[0].timeout).toBe(8000)
  expect(await fetchTrafficTrends({ ...dateQuery, signal })).toEqual(trends)
  expect(seen[1].signal).toBe(signal); expect(seen[1].timeout).toBe(8000)
  await updateManagedRuleSetContent(7, 'example.test', 2)
  expect(JSON.parse(seen[2].data)).toEqual({ content: 'example.test', expected_revision: 2 })
  expect(seen[2].timeout).toBe(30000)
})

it.each([
  ['access', (signal: AbortSignal) => fetchSubscriptionAccess(3, { signal })],
  ['admin access', (signal: AbortSignal) => fetchOrderSubscriptionAccess(10, { signal })],
  ['rule page', (signal: AbortSignal) => fetchManagedRuleSetsPage({}, { signal })],
  ['references', (signal: AbortSignal) => fetchAdminEntityReferences({ signal })],
  ['trends', (signal: AbortSignal) => fetchTrafficTrends({ ...dateQuery, signal })],
  ['fair use', (signal: AbortSignal) => fetchFairUseMetrics(3, signal)],
  ['traffic usage', (signal: AbortSignal) => fetchTrafficUsagePage({}, false, { signal })],
  ['node series', (signal: AbortSignal) => fetchTrafficNodeSeries({}, true, { signal })],
] as const)('honors pre-cancelled %s reads without reaching the actual transport', async (_name, invoke) => {
  const controller = new AbortController(); controller.abort()
  const transport = vi.fn<AxiosAdapter>(async config => response(config, {}))
  authenticatedAPI.defaults.adapter = transport
  await expect(invoke(controller.signal)).rejects.toMatchObject({ code: 'ERR_CANCELED' })
  expect(transport).not.toHaveBeenCalled()
  expect(isAuthSessionExpired()).toBe(false)
})

it('keeps a plugin-proof 401 separate from login expiry and verifies the main session without replaying the rule write', async () => {
  const seen: string[] = []
  const interceptor = authenticatedAPI.interceptors.request.use(config => {
    if (config.url === '/admin/subscription-rule-sets') config.headers['X-Plugin-Session'] = 'expired-plugin-proof'
    return config
  })
  authenticatedAPI.defaults.adapter = async config => {
    seen.push(config.url || '')
    if (config.url === '/auth/me') return response(config, { id: 1 })
    return reject(401, config, { error: { version: 1, code: 'unauthenticated' } })
  }
  try {
    await expect(createManagedRuleSet(request)).rejects.toMatchObject({ response: { status: 401 } })
    await vi.waitFor(() => expect(seen.filter(path => path === '/auth/me')).toHaveLength(1))
    expect(seen.filter(path => path === '/admin/subscription-rule-sets')).toHaveLength(1)
    expect(isAuthSessionExpired()).toBe(false)
  } finally { authenticatedAPI.interceptors.request.eject(interceptor) }
})

it('retains ordinary rule detail/content response contracts on the shared boundary', async () => {
  const item = { id: 7, revision: 2, name: 'Rules' }
  authenticatedAPI.defaults.adapter = async config => response(config, item)
  expect(await fetchManagedRuleSet(7)).toEqual(item)
  expect(await fetchManagedRuleSetContent(7)).toEqual(item)
})


it('preserves Fair Use observation-only policy bodies, explicit evaluation and cancellation without auto-dispatch', async () => {
  const seen: InternalAxiosRequestConfig[] = []
  const data = { configured: true, effective: policy, source: { scope_type: 'subscription', scope_id: 3 } }
  authenticatedAPI.defaults.adapter = async config => { seen.push(config); return response(config, data) }
  const signal = new AbortController().signal
  expect(await fetchFairUsePolicy(3, signal)).toEqual(data)
  expect(seen[0].signal).toBe(signal)
  await updateSubscriptionFairUsePolicy(3, { ...policy, enforcement_mode: 'must-not-enable-enforcement' }, false)
  expect(seen[1].method).toBe('put')
  const body = JSON.parse(seen[1].data)
  expect(body).toMatchObject({ enabled: false, expected_revision: 4, enforcement_mode: 'observe', connection_start_threshold: 100, working_node_threshold: 4 })
  expect(body).not.toHaveProperty('scope_id')
  expect(body).not.toHaveProperty('revision')
  await evaluateSubscriptionFairUse(3)
  expect(seen[2].method).toBe('post')
  expect(seen[2].url).toBe('/admin/subscriptions/3/fair-use/evaluate')
  expect(seen.every(config => config.timeout === 30000)).toBe(true)
})

it('retains explicit missing-envelope errors for protected usage and system reads', async () => {
  authenticatedAPI.defaults.adapter = async config => ({ ...response(config, {}), data: {} })
  await expect(fetchTrafficUsagePage()).rejects.toThrow('流量数据响应缺少 data')
  await expect(fetchAdminSystemInfo()).rejects.toThrow('系统信息响应缺少 data')
})

it('retains HTTP status/message and shared auth codes rather than wrapping blocked Fair Use calls in plain errors', async () => {
  authenticatedAPI.defaults.adapter = async config => reject(403, config, { message: '管理员权限不足' })
  const failure = await fetchFairUsePolicy(3).catch(cause => cause)
  expect(failure).toMatchObject({ status: 403, response: { status: 403 }, message: '管理员权限不足' })
  expect(isAuthSessionExpired()).toBe(false)
  authenticatedAPI.defaults.adapter = async config => reject(401, config)
  await fetchFairUsePolicy(3).catch(() => undefined)
  const transport = vi.fn<AxiosAdapter>(async config => response(config, {}))
  authenticatedAPI.defaults.adapter = transport
  await expect(evaluateSubscriptionFairUse(3)).rejects.toMatchObject({ code: 'AUTH_SESSION_EXPIRED' })
  expect(transport).not.toHaveBeenCalled()
})


it('discards a previous-account administrator subscription-link response without exposing the credential or replaying the POST', async () => {
  setAuthToken('previous-account')
  const transport = vi.fn<AxiosAdapter>(async config => {
    expect(config.url).toBe('/admin/orders/10/subscription-access')
    expect(config.method).toBe('post')
    setAuthToken('replacement-account')
    return response(config, access)
  })
  authenticatedAPI.defaults.adapter = transport
  await expect(fetchOrderSubscriptionAccess(10)).rejects.toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
  await Promise.resolve()
  expect(transport).toHaveBeenCalledOnce()
  expect(isAuthSessionExpired()).toBe(false)
})

it('does not wrap or replay a stale Fair Use policy-write response after the account changes', async () => {
  setAuthToken('previous-account')
  const transport = vi.fn<AxiosAdapter>(async config => {
    setAuthToken('replacement-account')
    return response(config, { configured: true, effective: policy })
  })
  authenticatedAPI.defaults.adapter = transport
  await expect(updateSubscriptionFairUsePolicy(3, policy, false)).rejects.toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
  expect(getAuthToken()).toBe('replacement-account')
  expect(isAuthSessionExpired()).toBe(false)
  expect(transport).toHaveBeenCalledOnce()
})
