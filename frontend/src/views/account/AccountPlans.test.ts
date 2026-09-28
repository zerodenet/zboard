import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { previewOrder, createOrder, fetchAccountSubscriptionsPage, fetchPlanCatalogItem, fetchPlanCatalogPage, fetchPlanCatalogSKUs } from '../../api/client'
import CommercePlanDetail from '../../components/CommercePlanDetail.vue'
import CommercePlanCard from '../../components/CommercePlanCard.vue'
import TablePager from '../../components/TablePager.vue'
import AccountPlans from './AccountPlans.vue'

vi.mock('../../api/client', () => ({
  previewOrder: vi.fn(), createOrder: vi.fn(), fetchAccountSubscriptionsPage: vi.fn(),
  fetchPlanCatalogItem: vi.fn(), fetchPlanCatalogPage: vi.fn(), fetchPlanCatalogSKUs: vi.fn(),
}))
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
function sku(id: number) { return { id, name: `SKU ${id}`, price_cents: 100, currency: 'CNY', billing_unit: 'month', billing_value: 1 } as any }
function plan(id: number) { return { id, name: `Plan ${id}`, slug: `plan-${id}`, description: `商品 ${id} 的详细说明`, traffic_bytes: 100, active_sku_count: 105, primary_sku: sku(id) } as any }
function subscription(id: number) {
  return { id, plan_id: id, plan_name: `Subscription ${id}`, sku_name: 'Monthly', status: 'active', end_at: '2027-01-01T00:00:00Z', flow_used: 1, flow_total: 100 } as any
}
function page(items: any[], total = items.length, offset = 0, limit = 25) {
  return { items, total, page: { offset, limit, total }, aggregates: {}, facets: {} } as any
}

describe('account catalog bounded reads and route isolation', () => {
  let wrapper: VueWrapper | undefined
  let router: Router
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(previewOrder).mockResolvedValue({ order_type: 'upgrade', amount_cents: 1500, credit_amount: 1000, payable_amount: 500, currency: 'CNY', time_credit: 1000, traffic_credit: 1000, traffic_bytes: 150 * 1024 ** 3, used_bytes: 0, end_at: '2027-01-01T00:00:00Z', quote_fingerprint: 'quote-token' })
    vi.mocked(fetchAccountSubscriptionsPage).mockImplementation(async params => params?.subscriptionId
      ? page([subscription(params.subscriptionId)]) : page([subscription(105)], 105, params?.offset, params?.limit))
    vi.mocked(fetchPlanCatalogPage).mockResolvedValue(page(Array.from({ length: 9 }, (_, i) => plan(i + 1)), 12))
    vi.mocked(fetchPlanCatalogItem).mockImplementation(async id => plan(id))
    vi.mocked(fetchPlanCatalogSKUs).mockImplementation(async (_id, params) => {
      const offset = params?.anchorId ? 100 : params?.offset || 0
      return page([sku(offset + 1), sku(offset + 2), sku(offset + 3), sku(offset + 4), sku(offset + 5)], 105, offset, params?.limit)
    })
  })
  afterEach(() => { wrapper?.unmount(); wrapper = undefined })
  async function open(path = '/account/plans') {
    router = createRouter({ history: createMemoryHistory(), routes: [
      { path: '/account/plans', component: { template: '<div />' } },
      { path: '/account/orders', component: { template: '<div />' } },
    ] })
    await router.push(path)
    await router.isReady()
    wrapper = mount(AccountPlans, { global: { plugins: [router], stubs: { CommercePlanDetail: true } } })
    await flushPromises()
  }
  it('renders nine cards with two reads and no per-card SKU request', async () => {
    await open()
    expect(wrapper!.findAllComponents(CommercePlanCard)).toHaveLength(9)
    expect(wrapper!.get('.storefront-plan-card__description').text()).toBe('商品 1 的详细说明')
    expect(fetchPlanCatalogPage).toHaveBeenCalledOnce()
    expect(fetchAccountSubscriptionsPage).toHaveBeenCalledOnce()
    expect(fetchPlanCatalogSKUs).not.toHaveBeenCalled()
    expect(fetchPlanCatalogItem).not.toHaveBeenCalled()
  })
  it('accepts a public purchase selection directly at the account checkout', async () => {
    await open('/account/plans?operation=purchase&plan=1&sku=105&step=checkout')
    expect(wrapper!.find('.purchase-checkout').exists()).toBe(true)
    expect(wrapper!.text()).toContain('确认创建订单')
    expect(router.currentRoute.value.query.sku).toBe('105')
  })
  it('does not block catalog rendering on a slow subscription list', async () => {
    vi.mocked(fetchAccountSubscriptionsPage).mockReturnValue(deferred<any>().promise)
    await open()
    expect(wrapper!.text()).toContain('Plan 1')
    expect(wrapper!.text()).toContain('正在加载订阅')
  })
  it('opens renewal details directly without another catalog or per-card request', async () => {
    await open()
    await wrapper!.findAll('button').find(item => item.text() === '续费')!.trigger('click')
    await flushPromises()
    expect(wrapper!.getComponent(CommercePlanDetail).props('mode')).toBe('renew')
    expect(wrapper!.getComponent(CommercePlanDetail).props('plan').description).toBe('商品 105 的详细说明')
    expect(fetchPlanCatalogPage).toHaveBeenCalledOnce()
    expect(fetchPlanCatalogSKUs).toHaveBeenCalledOnce()
    expect(router.currentRoute.value.query.subscription).toBe('105')
  })
  it('does not display a failed read as an empty catalog or empty subscriptions', async () => {
    vi.mocked(fetchAccountSubscriptionsPage).mockRejectedValue(new Error('offline'))
    vi.mocked(fetchPlanCatalogPage).mockRejectedValue(new Error('offline'))
    await open()
    expect(wrapper!.text()).toContain('套餐目录加载失败')
    expect(wrapper!.text()).not.toContain('暂无可购买套餐')
    expect(wrapper!.text()).not.toContain('当前没有有效订阅')
  })
  it('paginates the eligible subscription set instead of merging status previews', async () => {
    await open()
    const pager = wrapper!.findAllComponents(TablePager).find(item => item.props('total') === 105)!
    pager.vm.$emit('change', { offset: 102, limit: 6 })
    await flushPromises()
    expect(fetchAccountSubscriptionsPage).toHaveBeenLastCalledWith(
      { eligibleFor: 'manage', offset: 102, limit: 6 }, expect.objectContaining({ signal: expect.any(AbortSignal) }),
    )
    expect(router.currentRoute.value.query.subscription_page).toBe('18')
    expect(fetchPlanCatalogPage).toHaveBeenCalledOnce()
  })
  it('resolves an old target by ID and filters change candidates before pagination', async () => {
    await open('/account/plans?operation=change&subscription=1&page=2')
    expect(fetchAccountSubscriptionsPage).toHaveBeenCalledWith(
      { subscriptionId: 1, eligibleFor: 'change', offset: 0, limit: 1 }, expect.anything(),
    )
    expect(fetchPlanCatalogPage).toHaveBeenLastCalledWith(expect.objectContaining({
      operation: 'change', excludePlanId: 1, offset: 9, limit: 9,
    }), expect.anything())
    expect(wrapper!.text()).toContain('Subscription 1')
    expect(router.currentRoute.value.query.subscription).toBe('1')
  })
  it('keeps an unavailable target explicit and does not silently select another', async () => {
    vi.mocked(fetchAccountSubscriptionsPage).mockImplementation(async params => params?.subscriptionId ? page([]) : page([subscription(105)]))
    await open('/account/plans?operation=renew&subscription=1')
    expect(wrapper!.text()).toContain('目标订阅不存在或不适用于本次操作')
    expect(router.currentRoute.value.query.subscription).toBe('1')
    expect(fetchPlanCatalogPage).not.toHaveBeenCalled()
  })
  it('locates a deep-linked SKU beyond 100 and navigates actual SKU pages', async () => {
    await open('/account/plans?operation=renew&subscription=1&plan=1&sku=105')
    const detail = wrapper!.getComponent(CommercePlanDetail)
    expect(detail.props('selectedSkuId')).toBe(105)
    expect(detail.props('skuOffset')).toBe(100)
    expect(detail.props('skuTotal')).toBe(105)
    expect(fetchPlanCatalogSKUs).toHaveBeenLastCalledWith(1, {
      operation: 'renew', offset: 0, limit: 25, anchorId: 105,
    }, expect.anything())
    detail.vm.$emit('change-sku-page', { offset: 75, limit: 25 })
    await flushPromises()
    expect(fetchPlanCatalogSKUs).toHaveBeenLastCalledWith(1, {
      operation: 'renew', offset: 75, limit: 25, anchorId: undefined,
    }, expect.anything())
    expect(fetchPlanCatalogItem).toHaveBeenCalledOnce()
    expect(router.currentRoute.value.query.sku).toBe('76')
  })
  it('ignores a late target lookup after returning to purchase', async () => {
    const late = deferred<any>()
    vi.mocked(fetchAccountSubscriptionsPage).mockImplementation(async params => params?.subscriptionId ? late.promise : page([]))
    await open('/account/plans?operation=renew&subscription=1&plan=1')
    const signal = vi.mocked(fetchAccountSubscriptionsPage).mock.calls.find(([params]) => params?.subscriptionId)?.[1]?.signal
    await router.push('/account/plans')
    await flushPromises()
    expect(signal?.aborted).toBe(true)
    late.resolve(page([subscription(1)]))
    await flushPromises()
    expect(fetchPlanCatalogSKUs).not.toHaveBeenCalled()
    expect(wrapper!.text()).toContain('购买新的订阅')
  })
  it('uses the server credit and binds the quote to order creation', async () => {
    await open('/account/plans?operation=change&subscription=1&plan=2&step=checkout')
    expect(previewOrder).toHaveBeenCalledWith(1, 1, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper!.get('.purchase-checkout__summary').text()).toContain('5.00')
    expect(wrapper!.get('.purchase-checkout__summary').text()).toContain('原套餐抵扣')
    await wrapper!.findAll('button').find(item => item.text() === '确认创建订单')!.trigger('click')
    await flushPromises()
    expect(createOrder).toHaveBeenCalledWith(1, { orderType: 'upgrade', targetSubscriptionId: 1, quoteFingerprint: 'quote-token' })
    expect(router.currentRoute.value.path).toBe('/account/orders')
  })
  it('offers reset for an exhausted timed subscription and uses its reset SKU', async () => {
    const exhausted = { ...subscription(105), status: 'expired', flow_used: 100 }
    vi.mocked(fetchAccountSubscriptionsPage).mockResolvedValue(page([exhausted]))
    vi.mocked(previewOrder).mockResolvedValue({ order_type: 'traffic_reset', amount_cents: 200, credit_amount: 0, payable_amount: 200, currency: 'CNY', time_credit: 0, traffic_credit: 0, traffic_bytes: 100 * 1024 ** 3, used_bytes: 100, end_at: exhausted.end_at, quote_fingerprint: 'reset-token' })
    await open()
    await wrapper!.findAll('button').find(item => item.text() === '重置流量')!.trigger('click')
    await flushPromises()
    expect(fetchAccountSubscriptionsPage).toHaveBeenCalledWith(expect.objectContaining({ subscriptionId: 105, eligibleFor: 'reset' }), expect.anything())
    expect(fetchPlanCatalogSKUs).toHaveBeenLastCalledWith(105, expect.objectContaining({ operation: 'reset' }), expect.anything())
    expect(wrapper!.getComponent(CommercePlanDetail).props('mode')).toBe('reset')
    wrapper!.getComponent(CommercePlanDetail).vm.$emit('continue')
    await flushPromises()
    expect(wrapper!.text()).toContain('恢复可用流量')
    expect(wrapper!.text()).toContain('100 GB')
    expect(wrapper!.text()).toContain('保持不变')
    await wrapper!.findAll('button').find(item => item.text() === '确认创建订单')!.trigger('click')
    await flushPromises()
    expect(createOrder).toHaveBeenCalledWith(1, { orderType: 'traffic_reset', targetSubscriptionId: 105, quoteFingerprint: 'reset-token' })
  })
  it('ignores a late quote after returning to the overview', async () => {
    const late = deferred<any>()
    vi.mocked(previewOrder).mockReturnValue(late.promise)
    await open('/account/plans?operation=change&subscription=1&plan=2')
    const signal = vi.mocked(previewOrder).mock.calls[0]?.[2]?.signal
    await router.push('/account/plans')
    await flushPromises()
    late.resolve({ payable_amount: 1, quote_fingerprint: 'stale' })
    await flushPromises()
    expect(signal?.aborted).toBe(true)
    expect(wrapper!.findComponent(CommercePlanDetail).exists()).toBe(false)
    expect(createOrder).not.toHaveBeenCalled()
  })
  it('does not resurrect details after route exit or unmount', async () => {
    const late = deferred<any>()
    vi.mocked(fetchPlanCatalogSKUs).mockReturnValue(late.promise)
    await open('/account/plans?plan=1')
    await router.push('/account/plans')
    await flushPromises()
    expect(vi.mocked(fetchPlanCatalogSKUs).mock.calls[0]?.[2]?.signal?.aborted).toBe(true)
    late.resolve(page([sku(1)]))
    await flushPromises()
    expect(wrapper!.findComponent(CommercePlanDetail).exists()).toBe(false)
    expect(createOrder).not.toHaveBeenCalled()
  })
})
