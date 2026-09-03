import PrimeVue from 'primevue/config'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fetchPlanCatalogItem, fetchPlanCatalogPage, fetchPlanCatalogSKUs } from '../api/client'
import CommercePlanDetail from '../components/CommercePlanDetail.vue'
import PublicPlans from './PublicPlans.vue'

vi.mock('../api/client', () => ({ fetchPlanCatalogItem: vi.fn(), fetchPlanCatalogPage: vi.fn(), fetchPlanCatalogSKUs: vi.fn() }))
vi.mock('../stores/app', () => ({ useAppStore: () => ({ isAuthenticated: false, siteProfile: { policyDocuments: [] } }) }))
function page(id = 1, offset = 0) {
  return { items: [{ id, name: `SKU ${id}`, price_cents: 100, currency: 'CNY', billing_unit: 'month', billing_value: 1 }],
    total: 105, page: { total: 105, offset, limit: 25 } } as any
}
describe('public catalog navigation and SKU pager', () => {
  let wrapper: VueWrapper | undefined, router: Router
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(fetchPlanCatalogPage).mockResolvedValue({ items: [], total: 0 } as any)
    vi.mocked(fetchPlanCatalogItem).mockImplementation(async id => ({ id, name: `Plan ${id}`, slug: 'fixture', traffic_bytes: 100 } as any))
    vi.mocked(fetchPlanCatalogSKUs).mockImplementation(async (_id, params) => page(params?.anchorId || (params?.offset || 0) + 1, params?.anchorId ? 100 : params?.offset))
  })
  afterEach(() => { wrapper?.unmount(); wrapper = undefined })
  async function open(path: string) {
    router = createRouter({ history: createMemoryHistory(), routes: [
      { path: '/plans', component: { template: '<div />' } }, { path: '/login', component: { template: '<div />' } },
    ] })
    await router.push(path)
    await router.isReady()
    wrapper = mount(PublicPlans, { global: { plugins: [router, PrimeVue] } })
    await flushPromises()
  }
  it('renders the real SKU pager, changes pages and preserves the selected deep link', async () => {
    await open('/plans?plan=1&sku=105')
    expect(wrapper!.getComponent(CommercePlanDetail).props('selectedSkuId')).toBe(105)
    expect(wrapper!.text()).toContain('105 个可用规格')
    await wrapper!.get('button[aria-label="上一页"]').trigger('click')
    await flushPromises()
    expect(fetchPlanCatalogSKUs).toHaveBeenLastCalledWith(1, expect.objectContaining({ offset: 75, anchorId: undefined }), expect.anything())
    expect(router.currentRoute.value.query.sku).toBe('76')
    expect(fetchPlanCatalogItem).toHaveBeenCalledOnce()
  })
  it('does not offer checkout or an empty-state claim when a SKU page request fails', async () => {
    await open('/plans?plan=1')
    vi.mocked(fetchPlanCatalogSKUs).mockRejectedValueOnce(new Error('offline'))
    await wrapper!.get('button[aria-label="下一页"]').trigger('click')
    await flushPromises()
    expect(wrapper!.text()).toContain('规格加载失败')
    expect(wrapper!.text()).not.toContain('当前没有可用规格')
    const checkout = wrapper!.findAll('button').find(item => item.text().includes('继续结算'))!
    expect(checkout.attributes('disabled')).toBeDefined()
    await wrapper!.findAll('button').find(item => item.text().includes('重试规格'))!.trigger('click')
    await flushPromises()
    expect(wrapper!.getComponent(CommercePlanDetail).props('selectedSkuId')).toBe(26)
  })
  it('reloads an off-page SKU when the same plan deep link changes', async () => {
    await open('/plans?plan=1')
    await router.push('/plans?plan=1&sku=105')
    await flushPromises()
    expect(wrapper!.getComponent(CommercePlanDetail).props('selectedSkuId')).toBe(105)
    expect(fetchPlanCatalogSKUs).toHaveBeenCalledTimes(2)
  })
  it('retains the SKU in the login redirect without submitting an order', async () => {
    await open('/plans?plan=1&sku=105')
    await wrapper!.findAll('button').find(item => item.text().includes('继续结算'))!.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.redirect).toBe('/account/plans?operation=purchase&plan=1&sku=105&step=detail')
  })
})
