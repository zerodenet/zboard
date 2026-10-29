import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { createPinia } from 'pinia'
import { type AxiosAdapter } from 'axios'
import { authenticatedAPI, clearAuthToken, setAuthToken } from '../api/client'
import { resetAuthSessionExpired } from '../utils/authSession'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import CursorPager from '../components/CursorPager.vue'
import Traffic from './Traffic.vue'
import AccountTraffic from './account/AccountTraffic.vue'

// Keep actual Axios, its authentication boundary, API readers and composables.
// One adapter supplies both former transport fixtures; browser fetch is forbidden.
const transport = vi.fn<AxiosAdapter>()
const originalAdapter = authenticatedAPI.defaults.adapter
const aggregate = { raw_bytes: 1024, used_bytes: 1024, user_count: 1, subscription_count: 1, node_count: 1, protocol_endpoint_count: 1 }
const range = '?from=2026-09-01&to=2026-09-02'
describe('traffic view HTTP read boundaries', () => {
  let wrapper: VueWrapper | undefined, router: Router
  let malformedPage: boolean
  beforeEach(() => {
    malformedPage = false
    clearAuthToken(); resetAuthSessionExpired(); setAuthToken('traffic-read-fixture')
    transport.mockReset()
    transport.mockImplementation(async config => {
      const path = config.url || ''
      const url = new URL(path, 'https://fixture.invalid'), query = url.searchParams
      if (config.method !== 'get') throw new Error('traffic read fixture received a mutation')
      const data = url.pathname.endsWith('/traffic/records')
        ? query.get('view') === 'usage_summary'
          ? { total: 1, aggregates: aggregate, bucket: 'hour', as_of: '2026-09-01T00:00:00Z' }
          : query.get('view') === 'node_series'
            ? { points: [], nodes: [], node_limit: 8, truncated: false, bucket: 'hour' }
            : malformedPage ? {} : { items: [{ id: 1, user_id: 1, node_id: 1, subscription_id: 1,
              record_at: '2026-09-01T00:00:00Z', used_bytes: 1024, raw_bytes: 1024, protocol_multiplier_milli: 1000,
            }], total: null, page: { total: null, limit: 25, next_cursor: 'next' }, aggregates: null, facets: {} }
        : path.includes('/trends')
          ? { points: [], subscriptions: [], peak_connections: null, record_count: 0, connection_sample_count: 0, truncated: false, as_of: '2026-09-01T00:00:00Z' }
          : path.includes('/entity-references')
            ? { users: {}, subscriptions: {}, nodes: {}, plans: {}, plan_skus: {}, orders: {}, protocol_endpoints: {}, targets: {} }
            : { items: [], total: 0, aggregates: {} }
      return { status: 200, statusText: 'OK', config, headers: {}, data: { data } }
    })
    authenticatedAPI.defaults.adapter = transport
    // An accidental return to a standalone fetch must fail this boundary test.
    vi.stubGlobal('fetch', vi.fn(async () => { throw new Error('internal traffic read bypassed authenticatedAPI') }))
  })
  afterEach(() => { wrapper?.unmount(); wrapper = undefined; authenticatedAPI.defaults.adapter = originalAdapter; clearAuthToken(); resetAuthSessionExpired(); vi.unstubAllGlobals() })
  async function open(admin: boolean) {
    router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }] })
    await router.push((admin ? '/admin/traffic' : '/account/traffic') + range + (admin ? '&section=details' : ''))
    await router.isReady()
    wrapper = mount(admin ? Traffic : AccountTraffic, { global: {
      plugins: [router, createPinia()], stubs: { NodeTrafficChart: true, TrafficObservabilityChart: true },
    } })
    await flushPromises()
    expect(wrapper.getComponent(CursorPager).props('count')).toBe(1)
  }
  it.each([
    { admin: false, field: 'subscription_id' }, { admin: true, field: 'subscription_id' },
    { admin: true, field: 'user_id' }, { admin: true, field: 'node_id' },
  ])('rejects invalid $field without broadening HTTP scope (admin=$admin)', async ({ admin, field }) => {
    await open(admin)
    const fetchCount = vi.mocked(fetch).mock.calls.length, transportCount = transport.mock.calls.length
    const path = router.currentRoute.value.path
    await router.push(`${path}${range}${admin ? '&section=details' : ''}&${field}=not-an-id`); await flushPromises()
    expect(fetch).toHaveBeenCalledTimes(fetchCount)
    expect(transport).toHaveBeenCalledTimes(transportCount)
    expect(wrapper!.getComponent(CursorPager).props('count')).toBe(0)
    expect(wrapper!.getComponent(CursorPager).props('total')).toBeUndefined()
    expect(wrapper!.text()).toContain('流量记录加载失败')
    expect(wrapper!.text()).not.toContain('还没有流量使用明细')
    await router.push(`${path}${range}${admin ? '&section=details' : ''}&${field}=130`); await flushPromises()
    expect(wrapper!.getComponent(CursorPager).props('count')).toBe(1)
    expect(wrapper!.text()).not.toContain('流量记录加载失败')
    const recordRequests = transport.mock.calls.map(([config]) => config.url || '').filter(path => {
      const url = new URL(path, 'https://fixture.invalid')
      return url.pathname.endsWith('/traffic/records') && !url.searchParams.has('view')
    })
    expect(recordRequests.at(-1)).toContain(`${field}=130`)
    expect(fetch).not.toHaveBeenCalled()
  })
  it.each([false, true])('renders a malformed HTTP page as an error, not empty history (admin=%s)', async admin => {
    await open(admin)
    malformedPage = true
    await router.push(`${router.currentRoute.value.path}${range}${admin ? '&section=details' : ''}&cursor=next`); await flushPromises()
    expect(wrapper!.getComponent(CursorPager).props('count')).toBe(0)
    expect(wrapper!.text()).toContain('流量记录加载失败')
    expect(wrapper!.text()).not.toContain('还没有流量使用明细')
    malformedPage = false
    await wrapper!.findAll('button').find(button => button.text().includes('重试记录'))!.trigger('click')
    await flushPromises()
    expect(wrapper!.getComponent(CursorPager).props('count')).toBe(1)
    expect(wrapper!.text()).not.toContain('流量记录加载失败')
  })
})
