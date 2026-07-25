import PrimeVue from 'primevue/config'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fetchProtocolEndpointSelection, fetchProtocolEndpointsPage } from '../api/client'
import EndpointMultiLookup from './EndpointMultiLookup.vue'

vi.mock('../api/client', () => ({
  fetchProtocolEndpointsPage: vi.fn(async (params: { ids?: number[]; limit?: number } = {}) => {
    const ids = params.ids || [1, 2, 3]
    return {
      items: ids.map(id => ({
        id,
        node_id: Math.ceil(id / 10),
        node_name: `Node ${Math.ceil(id / 10)}`,
        name: `Endpoint ${id}`,
        protocol: 'ss',
        address: `edge-${id}.example.com`,
        port: 10000 + id,
        public_port: 10000 + id,
        multiplier_milli: 1000,
        is_active: true,
      })),
      page: { offset: 0, limit: params.limit || 25, total: ids.length, has_more: false },
      aggregates: {},
      facets: {},
      total: ids.length,
      offset: 0,
      limit: params.limit || 25,
    }
  }),
  fetchProtocolEndpointSelection: vi.fn(async () => ({
    ids: [1, 2, 3],
    total: 3,
    resolved_at: '2026-07-24T00:00:00Z',
  })),
}))

describe('EndpointMultiLookup', () => {
  beforeEach(() => {
    vi.mocked(fetchProtocolEndpointsPage).mockClear()
    vi.mocked(fetchProtocolEndpointSelection).mockClear()
  })

  it('renders and hydrates only one bounded selected-member page at a time', async () => {
    const ids = Array.from({ length: 5000 }, (_, index) => index + 1)
    const wrapper = mount(EndpointMultiLookup, {
      props: { modelValue: ids },
      global: { plugins: [PrimeVue] },
    })
    await flushPromises()

    expect(wrapper.findAll('.selection-chips > span')).toHaveLength(50)
    expect(wrapper.text()).toContain('第 1–50 项，共 5000 项')
    const hydrationCalls = vi.mocked(fetchProtocolEndpointsPage).mock.calls
      .map(([params]) => params?.ids)
      .filter((value): value is number[] => Array.isArray(value))
    expect(hydrationCalls).toEqual([ids.slice(0, 50)])

    const next = wrapper.get('nav[aria-label="已选协议端点分页"] button:last-child')
    await next.trigger('click')
    await flushPromises()

    expect(wrapper.findAll('.selection-chips > span')).toHaveLength(50)
    expect(wrapper.text()).toContain('第 51–100 项，共 5000 项')
    expect(wrapper.text()).toContain('Endpoint 51')
    expect(hydrationCalls).toHaveLength(1)
    const nextHydrationCalls = vi.mocked(fetchProtocolEndpointsPage).mock.calls
      .map(([params]) => params?.ids)
      .filter((value): value is number[] => Array.isArray(value))
    expect(nextHydrationCalls).toEqual([ids.slice(0, 50), ids.slice(50, 100)])
  })

  it('adds and removes the complete server-resolved filter snapshot without paging through result details', async () => {
    const wrapper = mount(EndpointMultiLookup, {
      props: { modelValue: [5] },
      global: { plugins: [PrimeVue] },
    })
    await flushPromises()

    const actions = wrapper.findAll('.lookup-scope-actions button')
    await actions[0].trigger('click')
    await flushPromises()

    expect(fetchProtocolEndpointSelection).toHaveBeenCalledWith(
      { q: undefined, active: true },
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    )
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual([5, 1, 2, 3])
    expect(wrapper.text()).toContain('筛选快照共 3 个端点，已加入 3 个待保存成员')
    expect(fetchProtocolEndpointsPage).toHaveBeenCalledTimes(2)

    await wrapper.setProps({ modelValue: [5, 1, 2, 3] })
    await actions[1].trigger('click')
    await flushPromises()

    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual([5])
    expect(wrapper.text()).toContain('已从待保存成员移除 3 个')
    expect(fetchProtocolEndpointsPage).toHaveBeenCalledTimes(2)
  })
})
