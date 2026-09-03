import PrimeVue from 'primevue/config'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import CursorPager from '../components/CursorPager.vue'
import Traffic from './Traffic.vue'
import AccountTraffic from './account/AccountTraffic.vue'

// Keep the actual API readers and composables; replace only HTTP transports.
const { get } = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('axios', () => ({ default: { create: () => ({
  get, interceptors: { request: { use: vi.fn() }, response: { use: vi.fn() } },
}) } }))
const aggregate = { raw_bytes: 1024, used_bytes: 1024, user_count: 1, subscription_count: 1, node_count: 1, protocol_endpoint_count: 1 }
const range = '?from=2026-09-01&to=2026-09-02'
describe('traffic view HTTP read boundaries', () => {
  let wrapper: VueWrapper | undefined, router: Router
  let malformedPage: boolean
  beforeEach(() => {
    malformedPage = false
    get.mockReset()
    get.mockImplementation(async (path: string) => ({ data: { data: path.includes('/trends')
      ? { points: [], subscriptions: [], peak_connections: null, record_count: 0, connection_sample_count: 0, truncated: false }
      : path.includes('/entity-references')
        ? { users: {}, subscriptions: {}, nodes: {}, plans: {}, plan_skus: {}, orders: {}, protocol_endpoints: {}, targets: {} }
        : { items: [], total: 0, aggregates: {} },
    } }))
    vi.stubGlobal('fetch', vi.fn().mockImplementation(async (path: string) => {
      const query = new URL(path, 'https://fixture.invalid').searchParams
      const data = query.get('view') === 'usage_summary'
        ? { total: 1, aggregates: aggregate, bucket: 'hour', as_of: '2026-09-01T00:00:00Z' }
        : query.get('view') === 'node_series'
          ? { points: [], nodes: [], node_limit: 8, truncated: false, bucket: 'hour' }
          : malformedPage ? {} : { items: [{ id: 1, user_id: 1, node_id: 1, subscription_id: 1,
            record_at: '2026-09-01T00:00:00Z', used_bytes: 1024, raw_bytes: 1024, protocol_multiplier_milli: 1000,
          }], total: null, page: { total: null, limit: 25, next_cursor: 'next' }, aggregates: null, facets: {} }
      return { ok: true, json: async () => ({ data }) }
    }))
  })
  afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.unstubAllGlobals() })
  async function open(admin: boolean) {
    router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }] })
    await router.push((admin ? '/admin/traffic' : '/account/traffic') + range)
    await router.isReady()
    wrapper = mount(admin ? Traffic : AccountTraffic, { global: {
      plugins: [router, PrimeVue], stubs: { NodeTrafficChart: true, TrafficObservabilityChart: true },
    } })
    await flushPromises()
    expect(wrapper.getComponent(CursorPager).props('count')).toBe(1)
  }
  it.each([
    { admin: false, field: 'subscription_id' }, { admin: true, field: 'subscription_id' },
    { admin: true, field: 'user_id' }, { admin: true, field: 'node_id' },
  ])('rejects invalid $field without broadening HTTP scope (admin=$admin)', async ({ admin, field }) => {
    await open(admin)
    const fetchCount = vi.mocked(fetch).mock.calls.length, getCount = get.mock.calls.length
    const path = router.currentRoute.value.path
    await router.push(`${path}${range}&${field}=not-an-id`); await flushPromises()
    expect(fetch).toHaveBeenCalledTimes(fetchCount)
    expect(get).toHaveBeenCalledTimes(getCount)
    expect(wrapper!.getComponent(CursorPager).props('count')).toBe(0)
    expect(wrapper!.getComponent(CursorPager).props('total')).toBeUndefined()
    expect(wrapper!.text()).toContain('流量记录加载失败')
    expect(wrapper!.text()).not.toContain('还没有流量使用明细')
    await router.push(`${path}${range}&${field}=130`); await flushPromises()
    expect(wrapper!.getComponent(CursorPager).props('count')).toBe(1)
    expect(wrapper!.text()).not.toContain('流量记录加载失败')
    expect(vi.mocked(fetch).mock.calls.at(-1)![0]).toContain(`${field}=130`)
  })
  it.each([false, true])('renders a malformed HTTP page as an error, not empty history (admin=%s)', async admin => {
    await open(admin)
    malformedPage = true
    await router.push(`${router.currentRoute.value.path}${range}&cursor=next`); await flushPromises()
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
