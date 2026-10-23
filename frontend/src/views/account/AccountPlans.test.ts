import { defineComponent, onMounted, reactive } from 'vue'
import { fetchPluginSlots } from '../../api/plugins'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { previewOrder, createOrder, fetchAccountSubscriptionsPage, fetchPlanCatalogItem, fetchPlanCatalogPage, fetchPlanCatalogSKUs, type OrderPreview } from '../../api/client'
import CommercePlanDetail from '../../components/CommercePlanDetail.vue'
import CommercePlanCard from '../../components/CommercePlanCard.vue'
import TablePager from '../../components/TablePager.vue'
import AccountPlans from './AccountPlans.vue'
import UiSelect from '../../components/UiSelect.vue'
import { fetchDiscountProviders } from '../../api/commerceDiscounts'

const actor = reactive({ id: 1 })
vi.mock('../../stores/app', () => ({ useAppStore: () => ({ user: actor }) }))
vi.mock('../../api/plugins', () => ({ fetchPluginSlots: vi.fn() }))
vi.mock('../../api/commerceDiscounts', async importOriginal => ({ ...await importOriginal<typeof import('../../api/commerceDiscounts')>(), fetchDiscountProviders: vi.fn() }))
const paymentAction = vi.fn()
vi.mock('../../api/client', () => ({
  previewOrder: vi.fn(), createOrder: vi.fn(), fetchAccountOrder: vi.fn(async id => ({ id, status: 'pending', payable_amount: 100, currency: 'CNY' })), fetchAccountSubscriptionsPage: vi.fn(),
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
  return { id, plan_id: id, plan_name: `Subscription ${id}`, sku_name: 'Monthly', status: 'active', can_renew: true, can_change: true, can_addon: true, can_reset: true, end_at: '2027-01-01T00:00:00Z', flow_used: 1, flow_total: 100 } as any
}
function page(items: any[], total = items.length, offset = 0, limit = 25) {
  return { items, total, page: { offset, limit, total }, aggregates: {}, facets: {} } as any
}
const discountProvider = { provider_id: 'native.offer', name: '新客礼遇', description: '首次购买享专属价格', operation: 'new', agreement_scope: 'initial_only' } as const
function discountedQuote(expiresAt = '2099-01-01T00:00:00Z'): OrderPreview {
  return { order_type: 'new', amount_cents: 100, base_price_minor: 100, discount_amount: 20, credit_amount: 0, payable_amount: 80, currency: 'CNY', time_credit: 0, traffic_credit: 0, traffic_bytes: 100, used_bytes: 0, quote_fingerprint: 'discount-quote',
    discount: { provider: { plugin_id: discountProvider.provider_id, signing_key_fingerprint: 'secret-signature', package_digest: 'secret-digest', generation: 1, config_revision: 1 }, offer: { status: 'available', input_fingerprint: 'private-input', currency: 'CNY', discount_minor: 20, expires_at: expiresAt, message: '欢迎首次购买', agreement: { id: 'first', version: '1', scope: 'initial_only', terms_hash: 'private-hash', terms_json: '{"private":"rules"}' } } } }
}
function recurringRenewalQuote(expiresAt = '2099-01-01T00:00:00Z'): OrderPreview {
  const value = discountedQuote(expiresAt)
  value.order_type = 'renewal'; value.currency = 'USD'; value.amount_cents = 60; value.base_price_minor = 60; value.payable_amount = 40
  value.quote_fingerprint = 'inherited-renewal'; value.discount!.offer.currency = 'USD'
  value.discount!.offer.agreement!.scope = 'recurring'; value.discount!.offer.message = '原长期会员约定'
  return value
}
function rejectedOrder(status: number, code: string, fields: Record<string, string> = {}) {
  return { response: { status, data: { message: '本次订单未创建', error: { version: 1, code, fields } } } }
}

describe('account catalog bounded reads and route isolation', () => {
  let wrapper: VueWrapper | undefined
  let router: Router
  beforeEach(() => {
    vi.clearAllMocks()
    actor.id = 1
    vi.mocked(fetchPluginSlots).mockResolvedValue([])
    vi.mocked(fetchDiscountProviders).mockResolvedValue([])
    paymentAction.mockResolvedValue({ checkout_url: 'https://cashier.example/pay' })
    localStorage.clear()
    vi.mocked(createOrder).mockResolvedValue({ id: 71 })
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
  afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.useRealTimers() })
  async function open(path = '/account/plans') {
    router = createRouter({ history: createMemoryHistory(), routes: [
      { path: '/account/plans', component: { template: '<div />' } },
      { path: '/account/orders', component: { template: '<div />' } },
      { path: '/account/orders/:id', component: { template: '<div />' } },
    ] })
    await router.push(path)
    await router.isReady()
    wrapper = mount(AccountPlans, { global: { plugins: [router], stubs: { CommercePlanDetail: true, PluginFrame: defineComponent({ emits: ['checkoutOptions'], setup(_, { emit, expose }) { expose({ checkoutAction: paymentAction }); onMounted(() => emit('checkoutOptions', [{ id: 'card', title: '测试支付', prepare_action: 'pay.prepare', reconcile_action: 'pay.check' }])); return () => null } }) } } })
    await flushPromises()
  }
  async function click(text: string) { await wrapper!.findAll('button').find(button => button.text() === text)!.trigger('click'); await flushPromises() }
  async function selectDiscount(code = 'WELCOME') {
    const picker = wrapper!.findAllComponents(UiSelect).find(select => (select.props('options') || []).some(option => option.value === discountProvider.provider_id))!
    picker.vm.$emit('update:modelValue', discountProvider.provider_id); await flushPromises()
    await wrapper!.get('#purchase-discount-code').setValue(code)
    await flushPromises()
  }
  async function discountedCheckout(expiresAt?: string) {
    vi.mocked(fetchDiscountProviders).mockResolvedValue([discountProvider])
    vi.mocked(previewOrder).mockResolvedValue(discountedQuote(expiresAt))
    await open('/account/plans?plan=1&step=checkout')
    await selectDiscount(); await click('核对优惠')
  }
  it('keeps normal purchase available when the independent discount directory fails', async () => {
    vi.mocked(fetchDiscountProviders).mockRejectedValue(new Error('offline'))
    await open('/account/plans?plan=1&step=checkout')
    expect(wrapper!.text()).toContain('优惠列表读取失败')
    expect(previewOrder).not.toHaveBeenCalled()
    expect(wrapper!.findAll('button').find(button => button.text() === '创建订单并选择付款方式')!.attributes('disabled')).toBeUndefined()
    await click('创建订单并选择付款方式')
    expect(createOrder).toHaveBeenCalledOnce()
    expect(vi.mocked(createOrder).mock.calls[0]![1]).not.toHaveProperty('discount')
  })
  it('defaults to no discount and shows named choices without requiring a provider identifier', async () => {
    vi.mocked(fetchDiscountProviders).mockResolvedValue([discountProvider])
    await open('/account/plans?plan=1&step=checkout')
    expect(wrapper!.text()).toContain('不使用优惠')
    expect(wrapper!.find('input[name="purchase-discount-provider"]').exists()).toBe(false)
    expect(previewOrder).not.toHaveBeenCalled()
    await selectDiscount('')
    expect(wrapper!.text()).toContain(discountProvider.description)
    expect(wrapper!.get('#purchase-discount-code').attributes('maxlength')).toBe('256')
    expect(wrapper!.findAll('button').find(button => button.text() === '创建订单并选择付款方式')!.attributes('disabled')).toBeDefined()
  })
  it('confirms server amounts and first-purchase terms before sending only references and the final fingerprint', async () => {
    await discountedCheckout()
    expect(previewOrder).toHaveBeenCalledWith(1, 0, { discount: { provider_id: discountProvider.provider_id, selection: 'WELCOME' }, signal: expect.any(AbortSignal) })
    expect(wrapper!.text()).toContain('欢迎首次购买')
    expect(wrapper!.text()).toContain('本次优惠仅限首单，续费基础价为 ¥1.00')
    expect(wrapper!.get('.purchase-checkout__summary').text()).toContain('0.80')
    expect(wrapper!.get('.purchase-checkout__summary').text()).toContain('本次优惠')
    expect(wrapper!.text()).not.toMatch(/secret-signature|secret-digest|private-hash|private-input|private.*rules/)
    expect(createOrder).not.toHaveBeenCalled()
    await click('创建订单并选择付款方式')
    expect(createOrder).toHaveBeenCalledWith(1, { orderType: 'new', targetSubscriptionId: undefined, quoteFingerprint: 'discount-quote', discount: { provider_id: discountProvider.provider_id, selection: 'WELCOME' }, idempotencyKey: expect.any(String) })
    expect(router.currentRoute.value.path).toBe('/account/orders/71')
    expect(paymentAction).not.toHaveBeenCalled()
  })
  it('clears a confirmed quote on code edits and keeps failed previews from restoring its price', async () => {
    await discountedCheckout()
    await wrapper!.get('#purchase-discount-code').setValue('CHANGED'); await flushPromises()
    expect(wrapper!.get('.purchase-checkout__summary').text()).toContain('正在核算')
    vi.mocked(previewOrder).mockRejectedValue(rejectedOrder(503, 'discount_unavailable'))
    await click('核对优惠')
    expect(wrapper!.text()).toContain('优惠报价失败')
    expect(wrapper!.get('.purchase-checkout__summary').text()).not.toContain('0.80')
    expect(createOrder).not.toHaveBeenCalled()
  })
  it('expires a displayed quote and requires a fresh explicit preview before creation', async () => {
    vi.useFakeTimers({ toFake: ['Date', 'setTimeout', 'clearTimeout'] })
    await discountedCheckout(new Date(Date.now() + 1000).toISOString())
    await vi.advanceTimersByTimeAsync(1001); await flushPromises()
    expect(wrapper!.text()).toContain('优惠报价已过期')
    expect(wrapper!.get('.purchase-checkout__summary').text()).not.toContain('0.80')
    expect(wrapper!.findAll('button').find(button => button.text() === '创建订单并选择付款方式')!.attributes('disabled')).toBeDefined()
    expect(createOrder).not.toHaveBeenCalled()
    expect(previewOrder).toHaveBeenCalledOnce()
  })
  it.each(['renew', 'change', 'addon', 'reset'])('does not advertise first-purchase discounts for %s', async operation => {
    await open(`/account/plans?operation=${operation}&subscription=1&plan=1&step=checkout`)
    expect(wrapper!.find('[aria-label="新购优惠"]').exists()).toBe(false)
    if (operation === 'renew') expect(fetchDiscountProviders).not.toHaveBeenCalled()
    else expect(fetchDiscountProviders).toHaveBeenCalledOnce()
    expect(wrapper!.findAllComponents(UiSelect).some(select => (select.props('options') || []).some(option => option.value === discountProvider.provider_id))).toBe(false)
  })
  it('confirms recurring purchase scope from the actual quote and explains locked base rather than a fixed discount', async () => {
    vi.mocked(fetchDiscountProviders).mockResolvedValue([{ ...discountProvider, agreement_scope: 'quote_required' }])
    const value = discountedQuote(); value.discount!.offer.agreement!.scope = 'recurring'
    vi.mocked(previewOrder).mockResolvedValue(value)
    await open('/account/plans?plan=1&step=checkout'); await selectDiscount()
    expect(wrapper!.text()).not.toContain('长期优惠约定：')
    await click('核对优惠')
    expect(wrapper!.text()).toContain('长期优惠约定：续费基础价已锁定为 ¥1.00')
    expect(wrapper!.text()).toContain('不保证固定')
    expect(wrapper!.text()).not.toContain('本次优惠仅限首单')
    vi.mocked(createOrder).mockRejectedValue(new Error('unknown response'))
    await click('创建订单并选择付款方式')
    expect(createOrder).toHaveBeenCalledWith(1, expect.objectContaining({ quoteFingerprint: 'discount-quote', discount: { provider_id: discountProvider.provider_id, selection: 'WELCOME' } }))
    expect(wrapper!.text()).toContain('长期优惠约定：续费基础价已锁定为 ¥1.00')
    expect(wrapper!.text()).not.toContain('本次优惠仅限首单')
    expect(wrapper!.text()).toContain('原确认优惠有效至')
  })
  it('shows inherited recurring renewal amounts and preserves its scope and currency on unknown reload without browser discount references', async () => {
    vi.useFakeTimers({ toFake: ['Date', 'setTimeout', 'clearTimeout'] })
    const originalExpiry = new Date(Date.now() + 1000).toISOString()
    vi.mocked(previewOrder).mockResolvedValue(recurringRenewalQuote(originalExpiry))
    vi.mocked(createOrder).mockRejectedValueOnce(new Error('response lost')).mockResolvedValueOnce({ id: 71 })
    const path = '/account/plans?operation=renew&subscription=1&plan=1&step=checkout'
    await open(path)
    expect(wrapper!.get('[aria-label="继承优惠"]').text()).toContain('续费基础价已锁定为 US$0.60')
    expect(wrapper!.get('.purchase-checkout__summary').text()).toContain('US$0.40')
    expect(wrapper!.get('.purchase-checkout__summary').text()).not.toContain('¥1.00')
    expect(wrapper!.find('#purchase-discount-code').exists()).toBe(false)
    expect(fetchDiscountProviders).not.toHaveBeenCalled()
    expect(previewOrder).toHaveBeenCalledWith(1, 1, { signal: expect.any(AbortSignal) })
    await click('创建订单并选择付款方式')
    const original = vi.mocked(createOrder).mock.calls[0]!
    expect(original[1]).not.toHaveProperty('discount')
    expect(original[1]).not.toHaveProperty('confirmation')
    expect(original[1]).toMatchObject({ orderType: 'renewal', quoteFingerprint: 'inherited-renewal' })
    const draft = JSON.parse(localStorage.getItem(Array.from({ length: localStorage.length }, (_, i) => localStorage.key(i)!).find(key => key.startsWith('zboard-checkout:'))!)!)
    expect(draft.request.confirmation.discount).toMatchObject({ scope: 'recurring', basePrice: 60, payable: 40, currency: 'USD', expiresAt: originalExpiry })
    await vi.advanceTimersByTimeAsync(1001); await flushPromises()
    vi.mocked(previewOrder).mockResolvedValue({ ...recurringRenewalQuote(), payable_amount: 9999 })
    wrapper!.unmount(); wrapper = undefined; await open(path)
    expect(previewOrder).toHaveBeenCalledOnce()
    expect(wrapper!.get('[aria-label="继承优惠"]').text()).toContain('长期优惠约定')
    expect(wrapper!.get('[aria-label="继承优惠"]').text()).toContain('原确认优惠有效至')
    expect(wrapper!.text()).not.toContain('继承的优惠报价已过期')
    expect(wrapper!.get('.purchase-checkout__summary').text()).toContain('US$0.40')
    expect(wrapper!.text()).not.toContain('99.99')
    await click('恢复上次订单')
    expect(vi.mocked(createOrder).mock.calls[1]).toEqual(original)
  })
  it('expires inherited renewal discounts and requires an explicit successful quote before creating', async () => {
    vi.useFakeTimers({ toFake: ['Date', 'setTimeout', 'clearTimeout'] })
    vi.mocked(previewOrder).mockResolvedValue(recurringRenewalQuote(new Date(Date.now() + 1000).toISOString()))
    await open('/account/plans?operation=renew&subscription=1&plan=1&step=checkout')
    await vi.advanceTimersByTimeAsync(1001); await flushPromises()
    expect(wrapper!.text()).toContain('继承的优惠报价已过期')
    expect(wrapper!.findAll('button').find(button => button.text() === '创建订单并选择付款方式')!.attributes('disabled')).toBeDefined()
    expect(createOrder).not.toHaveBeenCalled()
    vi.mocked(previewOrder).mockRejectedValue(rejectedOrder(409, 'subscription_quote_changed'))
    await click('重新报价')
    expect(wrapper!.get('.purchase-checkout__summary').text()).not.toContain('US$0.40')
    expect(createOrder).not.toHaveBeenCalled()
    vi.mocked(previewOrder).mockResolvedValue(recurringRenewalQuote())
    await click('重新报价')
    expect(createOrder).not.toHaveBeenCalled()
    await click('创建订单并选择付款方式')
    expect(createOrder).toHaveBeenCalledOnce()
  })
  it.each(['initial_only', 'unavailable', 'requires_confirmation'] as const)('blocks invalid inherited renewal %s without ordinary-price fallback', async invalid => {
    const value = recurringRenewalQuote()
    if (invalid === 'initial_only') value.discount!.offer.agreement!.scope = invalid
    else value.discount!.offer.status = invalid
    vi.mocked(previewOrder).mockResolvedValue(value)
    await open('/account/plans?operation=renew&subscription=1&plan=1&step=checkout')
    expect(wrapper!.text()).toContain('继承的优惠报价不可确认')
    expect(wrapper!.get('.purchase-checkout__summary').text()).not.toContain('¥1.00')
    expect(wrapper!.findAll('button').find(button => button.text() === '创建订单并选择付款方式')!.attributes('disabled')).toBeDefined()
    expect(createOrder).not.toHaveBeenCalled()
  })
  it('ignores an old discount quote after changing the SKU', async () => {
    vi.mocked(fetchDiscountProviders).mockResolvedValue([discountProvider])
    const late = deferred<OrderPreview>()
    vi.mocked(previewOrder).mockReturnValue(late.promise)
    await open('/account/plans?plan=1&step=checkout'); await selectDiscount(); await click('核对优惠')
    await router.push('/account/plans?plan=1&sku=2&step=checkout'); await flushPromises()
    late.resolve(discountedQuote()); await flushPromises()
    expect(wrapper!.text()).not.toContain('欢迎首次购买')
    expect(wrapper!.text()).toContain('不使用优惠')
    expect(createOrder).not.toHaveBeenCalled()
    expect(vi.mocked(previewOrder).mock.calls[0]![2]!.signal?.aborted).toBe(true)
  })
  it('clears prior-identity discounts before showing another account checkout', async () => {
    await discountedCheckout()
    actor.id = 2; await flushPromises()
    expect(wrapper!.text()).not.toContain('欢迎首次购买')
    expect(wrapper!.text()).toContain('不使用优惠')
    expect(wrapper!.get('.purchase-checkout__summary').text()).not.toContain('0.80')
    expect(createOrder).not.toHaveBeenCalled()
  })
  it('restores an uncertain discounted request after reload without previewing or creating automatically', async () => {
    await discountedCheckout()
    vi.mocked(createOrder).mockRejectedValueOnce(new Error('response lost')).mockResolvedValueOnce({ id: 71 })
    await click('创建订单并选择付款方式')
    const original = vi.mocked(createOrder).mock.calls[0]
    wrapper!.unmount(); wrapper = undefined
    await open('/account/plans?plan=1&step=checkout')
    expect(wrapper!.text()).toContain('恢复上次订单')
    expect(wrapper!.text()).toContain('新客礼遇')
    expect(wrapper!.get('.purchase-checkout__summary').text()).toContain('0.80')
    expect(createOrder).toHaveBeenCalledOnce()
    expect(previewOrder).toHaveBeenCalledOnce()
    await click('恢复上次订单')
    expect(vi.mocked(createOrder).mock.calls[1]).toEqual(original)
  })
  it.each(['renew', 'change'] as const)('retains the original %s confirmation after unknown creation and reload without showing or fetching a replacement quote', async operation => {
    const initial: OrderPreview = { order_type: operation === 'renew' ? 'renewal' : 'upgrade', amount_cents: 1500, credit_amount: 1000, payable_amount: 500, currency: 'CNY', time_credit: 1000, traffic_credit: 1000, traffic_bytes: 150, used_bytes: 0, quote_fingerprint: 'original-confirmation' }
    vi.mocked(previewOrder).mockResolvedValue(initial)
    vi.mocked(createOrder).mockRejectedValueOnce(new Error('response lost')).mockResolvedValueOnce({ id: 71 })
    const path = `/account/plans?operation=${operation}&subscription=1&plan=${operation === 'change' ? 2 : 1}&step=checkout`
    await open(path)
    await click('创建订单并选择付款方式')
    const original = vi.mocked(createOrder).mock.calls[0]
    expect(wrapper!.text()).toContain('恢复将沿用原订单内容和原确认金额')
    expect(wrapper!.get('.purchase-checkout__summary').text()).toContain('5.00')
    expect(wrapper!.text()).not.toContain('已确认优惠')
    expect(previewOrder).toHaveBeenCalledOnce()
    vi.mocked(previewOrder).mockResolvedValue({ ...initial, amount_cents: 9999, credit_amount: 8888, payable_amount: 1111, change_mode: 'full_term', settlement_amount: '777777', surplus_amount: '666666', additional_amount: '1111', quote_fingerprint: 'replacement-must-not-be-used' })
    wrapper!.unmount(); wrapper = undefined
    await open(path)
    expect(wrapper!.get('.purchase-checkout__summary').text()).toContain('5.00')
    expect(wrapper!.text()).not.toMatch(/99\.99|88\.88|11\.11|7777\.77|6666\.66/)
    expect(wrapper!.text()).not.toContain('旧权益结算总额')
    expect(previewOrder).toHaveBeenCalledOnce()
    expect(createOrder).toHaveBeenCalledOnce()
    await click('恢复上次订单')
    expect(vi.mocked(createOrder).mock.calls[1]).toEqual(original)
    expect(vi.mocked(createOrder).mock.calls[1]![1]).not.toHaveProperty('confirmation')
  })
  it.each(['purchase', 'renew', 'change', 'addon', 'reset'] as const)('shows an unknown original amount for a legacy %s attempt without borrowing a fresh SKU or quote price', async operation => {
    const intent = 'a'.repeat(32)
    const orderType = { purchase: 'new', renew: 'renewal', change: 'upgrade', addon: 'traffic_pack', reset: 'traffic_reset' }[operation]
    const request = { skuId: 1, orderType, ...(operation === 'purchase' ? {} : { targetSubscriptionId: 1, quoteFingerprint: 'legacy-confirmed' }) }
    localStorage.setItem(`zboard-checkout:1:${intent}`, JSON.stringify({ key: 'original-key', request }))
    await open(`/account/plans?operation=${operation}&plan=${operation === 'change' ? 2 : 1}&step=checkout&checkout_intent=${intent}${operation === 'purchase' ? '' : '&subscription=1'}`)
    expect(wrapper!.text()).toContain('恢复将沿用原订单内容和原确认金额')
    expect(wrapper!.get('.purchase-checkout__summary').text()).toContain('金额未知，请核对原订单')
    expect(wrapper!.get('.purchase-checkout__summary').text()).not.toContain('¥1.00')
    expect(wrapper!.text()).not.toContain('原套餐抵扣')
    expect(previewOrder).not.toHaveBeenCalled()
    expect(createOrder).not.toHaveBeenCalled()
    await click('恢复上次订单')
    const { skuId, ...options } = request
    expect(createOrder).toHaveBeenCalledWith(skuId, { ...options, idempotencyKey: 'original-key' })
  })
  it('blocks a stored JSON null recovery record without overwriting it or creating', async () => {
    const intent = 'b'.repeat(32), scope = `zboard-checkout:1:${intent}`
    localStorage.setItem(scope, 'null')
    await open(`/account/plans?operation=renew&subscription=1&plan=1&step=checkout&checkout_intent=${intent}`)
    expect(wrapper!.text()).toContain('订单创建记录不完整')
    expect(wrapper!.findAll('button').find(button => button.text() === '创建订单并选择付款方式')!.attributes('disabled')).toBeDefined()
    expect(localStorage.getItem(scope)).toBe('null')
    expect(previewOrder).not.toHaveBeenCalled()
    expect(createOrder).not.toHaveBeenCalled()
  })
  it.each(['unavailable', 'requires_confirmation'] as const)('does not provide a checkbox override for a %s preview', async status => {
    vi.mocked(fetchDiscountProviders).mockResolvedValue([discountProvider])
    const quote = discountedQuote(); quote.discount!.offer.status = status
    vi.mocked(previewOrder).mockResolvedValue(quote)
    await open('/account/plans?plan=1&step=checkout'); await selectDiscount(); await click('核对优惠')
    expect(wrapper!.text()).toContain('优惠报价失败')
    expect(wrapper!.find('input[type="checkbox"]').exists()).toBe(false)
    expect(wrapper!.findAll('button').find(button => button.text() === '创建订单并选择付款方式')!.attributes('disabled')).toBeDefined()
    expect(createOrder).not.toHaveBeenCalled()
  })
  it.each([
    new Error('response lost'), rejectedOrder(500, 'internal_error'), rejectedOrder(409, 'conflict'), rejectedOrder(409, 'subscription_quote_changed'), rejectedOrder(409, 'discount_confirmation_required'),
    rejectedOrder(400, 'validation_failed', { idempotency_key: '原请求不同，请核对已有订单' }),
  ])('preserves uncertain or conflicting creation requests for explicit recovery', async failure => {
    await discountedCheckout()
    vi.mocked(createOrder).mockRejectedValueOnce(failure).mockResolvedValueOnce({ id: 71 })
    await click('创建订单并选择付款方式')
    const first = vi.mocked(createOrder).mock.calls[0]
    expect(wrapper!.text()).toContain('恢复上次订单')
    expect(wrapper!.find('#purchase-discount-code').exists()).toBe(false)
    expect(wrapper!.get('.purchase-checkout__summary').text()).toContain('0.80')
    expect(previewOrder).toHaveBeenCalledOnce()
    expect(createOrder).toHaveBeenCalledOnce()
    await click('恢复上次订单')
    expect(vi.mocked(createOrder).mock.calls[1]).toEqual(first)
    expect(router.currentRoute.value.path).toBe('/account/orders/71')
  })
  it.each([[400, 'validation_failed'], [503, 'discount_unavailable'], [502, 'discount_invalid']] as const)('releases a guaranteed rollback %s/%s and requires a new quote and explicit creation with a new key', async (status, code) => {
    await discountedCheckout()
    vi.mocked(createOrder).mockRejectedValueOnce(rejectedOrder(status, code)).mockResolvedValueOnce({ id: 71 })
    await click('创建订单并选择付款方式')
    const first = vi.mocked(createOrder).mock.calls[0]![1]!
    expect(wrapper!.text()).not.toContain('恢复上次订单')
    expect(wrapper!.text()).toContain('请重新核对报价后确认创建')
    expect(wrapper!.get('.purchase-checkout__summary').text()).not.toContain('0.80')
    expect(createOrder).toHaveBeenCalledOnce()
    expect(previewOrder).toHaveBeenCalledOnce()
    vi.mocked(previewOrder).mockResolvedValue({ ...discountedQuote(), quote_fingerprint: 'replacement-quote' })
    await click('核对优惠')
    expect(createOrder).toHaveBeenCalledOnce()
    await click('创建订单并选择付款方式')
    expect(vi.mocked(createOrder).mock.calls[1]![1]!.idempotencyKey).not.toBe(first.idempotencyKey)
    expect(vi.mocked(createOrder).mock.calls[1]![1]!.quoteFingerprint).toBe('replacement-quote')
  })
  it('shows full new term, exact residual and wallet surplus before an explicit order action', async () => {
    vi.mocked(previewOrder).mockResolvedValue({ order_type: 'upgrade', change_mode: 'full_term', amount_cents: 2000, credit_amount: 2000, payable_amount: 2000, settlement_amount: '9007199254740993', current_residual: '9007199254740993', future_residual: '0', surplus_amount: '9007199254738993', additional_amount: '0', currency: 'CNY', time_credit: 0, traffic_credit: 0, traffic_bytes: 100, used_bytes: 0, end_at: '2027-02-01T00:00:00Z', quote_fingerprint: 'full-term-quote' })
    await open('/account/plans?plan=1&operation=change&subscription=105')
    wrapper!.getComponent(CommercePlanDetail).vm.$emit('continue'); await flushPromises()
    expect(wrapper!.text()).toContain('新套餐完整价格')
    expect(wrapper!.text()).toContain('CNY 90071992547409.93')
    expect(wrapper!.text()).toContain('CNY 90071992547389.93')
    expect(wrapper!.text()).toContain('按履约时间起算')
    expect(createOrder).not.toHaveBeenCalled()
    expect(paymentAction).not.toHaveBeenCalled()
  })
  it('creates an order below the confirmed amount before handing off to funding, without collecting cash', async () => {
    const reserve = vi.spyOn(window, 'open').mockReturnValue(null)
    await open('/account/plans?plan=1')
    wrapper!.getComponent(CommercePlanDetail).vm.$emit('continue'); await flushPromises()
    expect(wrapper!.find('.purchase-checkout__summary .checkout-payment').exists()).toBe(true)
    expect(wrapper!.find('.purchase-checkout__main .checkout-payment').exists()).toBe(false)
    expect(createOrder).not.toHaveBeenCalled()
    expect(fetchPluginSlots).not.toHaveBeenCalled()
    await wrapper!.findAll('button').find(button => button.text() === '创建订单并选择付款方式')!.trigger('click'); await flushPromises()
    expect(createOrder).toHaveBeenCalledOnce()
    expect(router.currentRoute.value.path).toBe('/account/orders/71')
    expect(paymentAction).not.toHaveBeenCalled()
    expect(reserve).not.toHaveBeenCalled()
    reserve.mockRestore()
  })
  it('defers plugin discovery until funding selection and never automatically creates or collects', async () => {
    vi.mocked(fetchPluginSlots).mockRejectedValue(new Error('offline'))
    await open('/account/plans?plan=1')
    wrapper!.getComponent(CommercePlanDetail).vm.$emit('continue'); await flushPromises()
    expect(fetchPluginSlots).not.toHaveBeenCalled()
    expect(createOrder).not.toHaveBeenCalled()
    expect(wrapper!.findAll('button').find(button => button.text() === '创建订单并选择付款方式')!.attributes('disabled')).toBeUndefined()
    expect(paymentAction).not.toHaveBeenCalled()
  })
  it('renders nine cards with two reads and no per-card SKU request', async () => {
    await open()
    expect(wrapper!.findAllComponents(CommercePlanCard)).toHaveLength(9)
    expect(wrapper!.get('.storefront-plan-card__description').text()).toBe('商品 1 的详细说明')
    expect(fetchPlanCatalogPage).toHaveBeenCalledOnce()
    expect(fetchAccountSubscriptionsPage).toHaveBeenCalledOnce()
    expect(fetchPlanCatalogSKUs).not.toHaveBeenCalled()
    expect(fetchPlanCatalogItem).not.toHaveBeenCalled()
  })
  it('renders only saleable operations returned by the backend', async () => {
    vi.mocked(fetchAccountSubscriptionsPage).mockResolvedValue(page([{ ...subscription(105), can_renew: true, can_change: false, can_addon: false, can_reset: false }]))
    await open()
    const actions = wrapper!.get('.commerce-subscription-actions')
    expect(actions.text()).toBe('续费')
    expect(fetchPlanCatalogSKUs).not.toHaveBeenCalled()
  })
  it('shows the renewal recovery deadline for a stopped service', async () => {
    vi.mocked(fetchAccountSubscriptionsPage).mockResolvedValue(page([{ ...subscription(105), status: 'expired', ended_at: '2026-09-27T00:00:00Z', end_reason: 'expired', renewal_until: '2026-10-04T00:00:00Z', can_change: false, can_reset: false, can_addon: false }]))
    await open()
    expect(wrapper!.get('.commerce-subscription-card').text()).toContain('服务已停止')
    expect(wrapper!.get('.commerce-subscription-actions').text()).toBe('续费恢复')
    await wrapper!.findAll('button').find(button => button.text() === '续费恢复')!.trigger('click')
    await flushPromises()
    expect(fetchAccountSubscriptionsPage).toHaveBeenCalledWith(expect.objectContaining({ subscriptionId: 105, eligibleFor: 'renew' }), expect.anything())
    expect(fetchPlanCatalogSKUs).toHaveBeenLastCalledWith(105, expect.objectContaining({ operation: 'renew' }), expect.anything())
  })
  it('keeps configured renewal and addon actions for an exhausted subscription', async () => {
    vi.mocked(fetchAccountSubscriptionsPage).mockResolvedValue(page([{ ...subscription(105), flow_used: 100, can_change: false, can_reset: false }]))
    await open()
    expect(wrapper!.get('.commerce-subscription-actions').text()).toBe('续费购买流量包')
  })
  it('allows a paid change with zero credit when no reset SKU is configured', async () => {
    vi.mocked(fetchAccountSubscriptionsPage).mockImplementation(async params => page([{ ...subscription(params?.subscriptionId || 105), flow_used: 100, can_renew: false, can_addon: false, can_reset: false, can_change: true }]))
    vi.mocked(previewOrder).mockResolvedValue({ order_type: 'upgrade', amount_cents: 1500, credit_amount: 0, payable_amount: 1500, currency: 'CNY', time_credit: 500, traffic_credit: 0, traffic_bytes: 150 * 1024 ** 3, used_bytes: 100, end_at: '2027-01-01T00:00:00Z', quote_fingerprint: 'exhausted-change' })
    await open()
    expect(wrapper!.get('.commerce-subscription-actions').text()).toBe('切换套餐')
    await router.push('/account/plans?operation=change&subscription=105&plan=2&step=checkout')
    await flushPromises()
    expect(wrapper!.get('.purchase-checkout__summary').text()).toContain('15.00')
    expect(wrapper!.get('.purchase-checkout__summary').text()).toContain('0.00')
    expect(createOrder).not.toHaveBeenCalled()
    await wrapper!.findAll('button').find(item => item.text() === '创建订单并选择付款方式')!.trigger('click')
    await flushPromises()
    expect(createOrder).toHaveBeenCalledWith(1, { orderType: 'upgrade', targetSubscriptionId: 105, quoteFingerprint: 'exhausted-change', idempotencyKey: expect.any(String) })
  })
  it('hides an empty management section while keeping the purchase catalog', async () => {
    vi.mocked(fetchAccountSubscriptionsPage).mockResolvedValue(page([]))
    await open()
    expect(wrapper!.find('#active-subscriptions-title').exists()).toBe(false)
    expect(wrapper!.find('#target-subscription-title').exists()).toBe(false)
    expect(wrapper!.text()).not.toContain('当前没有有效订阅')
    expect(wrapper!.find('.commerce-subscription-card').exists()).toBe(false)
    expect(wrapper!.findAllComponents(CommercePlanCard)).toHaveLength(9)
  })
  it('accepts a public purchase selection directly at the account checkout', async () => {
    await open('/account/plans?operation=purchase&plan=1&sku=105&step=checkout')
    expect(wrapper!.find('.purchase-checkout').exists()).toBe(true)
    expect(wrapper!.text()).toContain('创建订单并选择付款方式')
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
    expect(wrapper!.text()).not.toContain('当前没有可操作订阅')
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
  it('shows the server renewal price and binds its confirmation instead of the current SKU price', async () => {
    vi.mocked(previewOrder).mockResolvedValue({ order_type: 'renewal', amount_cents: 60, payable_amount: 60, credit_amount: 0, currency: 'CNY', time_credit: 0, traffic_credit: 0, traffic_bytes: 100, used_bytes: 1, quote_fingerprint: 'renewal-lock' })
    await open('/account/plans?operation=renew&subscription=1&plan=1&step=checkout')
    expect(previewOrder).toHaveBeenCalledWith(1, 1, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper!.get('.purchase-checkout__summary').text()).toContain('0.60')
    expect(wrapper!.get('.purchase-checkout__product').text()).toContain('原购买基础价')
    await wrapper!.findAll('button').find(item => item.text() === '创建订单并选择付款方式')!.trigger('click')
    await flushPromises()
    expect(createOrder).toHaveBeenCalledWith(1, { orderType: 'renewal', targetSubscriptionId: 1, quoteFingerprint: 'renewal-lock', idempotencyKey: expect.any(String) })
  })
  it('uses the server credit and binds the quote to order creation', async () => {
    await open('/account/plans?operation=change&subscription=1&plan=2&step=checkout')
    expect(previewOrder).toHaveBeenCalledWith(1, 1, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper!.get('.purchase-checkout__summary').text()).toContain('5.00')
    expect(wrapper!.get('.purchase-checkout__summary').text()).toContain('原套餐抵扣')
    await wrapper!.findAll('button').find(item => item.text() === '创建订单并选择付款方式')!.trigger('click')
    await flushPromises()
    expect(createOrder).toHaveBeenCalledWith(1, { orderType: 'upgrade', targetSubscriptionId: 1, quoteFingerprint: 'quote-token', idempotencyKey: expect.any(String) })
    expect(router.currentRoute.value.path).toBe('/account/orders/71')
  })
  it('offers reset for an exhausted timed subscription and uses its reset SKU', async () => {
    const exhausted = { ...subscription(105), status: 'expired', flow_used: 100, can_renew: false, can_change: false, can_addon: false }
    vi.mocked(fetchAccountSubscriptionsPage).mockResolvedValue(page([exhausted]))
    vi.mocked(previewOrder).mockResolvedValue({ order_type: 'traffic_reset', amount_cents: 200, credit_amount: 0, payable_amount: 200, currency: 'CNY', time_credit: 0, traffic_credit: 0, traffic_bytes: 100 * 1024 ** 3, used_bytes: 100, end_at: exhausted.end_at, quote_fingerprint: 'reset-token' })
    await open()
    await wrapper!.findAll('button').find(item => item.text() === '购买流量重置')!.trigger('click')
    await flushPromises()
    expect(fetchAccountSubscriptionsPage).toHaveBeenCalledWith(expect.objectContaining({ subscriptionId: 105, eligibleFor: 'reset' }), expect.anything())
    expect(fetchPlanCatalogSKUs).toHaveBeenLastCalledWith(105, expect.objectContaining({ operation: 'reset' }), expect.anything())
    expect(wrapper!.getComponent(CommercePlanDetail).props('mode')).toBe('reset')
    wrapper!.getComponent(CommercePlanDetail).vm.$emit('continue')
    await flushPromises()
    expect(wrapper!.text()).toContain('恢复可用流量')
    expect(wrapper!.text()).toContain('100 GB')
    expect(wrapper!.text()).toContain('保持不变')
    await wrapper!.findAll('button').find(item => item.text() === '创建订单并选择付款方式')!.trigger('click')
    await flushPromises()
    expect(createOrder).toHaveBeenCalledWith(1, { orderType: 'traffic_reset', targetSubscriptionId: 105, quoteFingerprint: 'reset-token', idempotencyKey: expect.any(String) })
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
  it.each([['change', 'upgrade'], ['reset', 'traffic_reset'], ['addon', 'traffic_pack']] as const)('confirms only a one-order %s offer and submits the original target and quote', async (operation, orderType) => {
    vi.mocked(fetchDiscountProviders).mockResolvedValue([{ ...discountProvider, agreement_scope: 'quote_required', operations: ['new', 'upgrade', 'traffic_reset', 'traffic_pack'] }])
    const value = discountedQuote(); value.order_type = orderType; value.discount!.offer.agreement!.scope = 'order_only'; value.discount!.offer.message = '本单优惠已核对'
    if (operation === 'change') Object.assign(value, { change_mode: 'full_term', credit_amount: 80, settlement_amount: '9000', surplus_amount: '8920', additional_amount: '0' })
    vi.mocked(previewOrder).mockImplementation(async (_sku, _target, options) => options?.discount ? value : { ...value, discount: undefined, discount_amount: 0, payable_amount: 100 })
    await open(`/account/plans?operation=${operation}&subscription=1&plan=${operation === 'change' ? 2 : 1}&step=checkout`)
    await selectDiscount()
    expect(wrapper!.findAll('button').find(button => button.text() === '创建订单并选择付款方式')!.attributes('disabled')).toBeDefined()
    await click('核对优惠')
    expect(previewOrder).toHaveBeenLastCalledWith(1, 1, { signal: expect.any(AbortSignal), discount: { provider_id: discountProvider.provider_id, selection: 'WELCOME' } })
    expect(wrapper!.get('[aria-label="本单优惠"]').text()).toContain('本次优惠仅用于本单')
    expect(wrapper!.text()).not.toContain('本次优惠仅限首单')
    if (operation === 'change') {
      expect(wrapper!.text()).toContain('换购后的续费基础价为 ¥1.00')
      expect(wrapper!.get('.purchase-checkout__summary').text()).toContain('89.20')
    } else expect(wrapper!.text()).toContain('不改变原订阅的续费基础价')
    vi.mocked(createOrder).mockRejectedValueOnce(new Error('response lost')).mockResolvedValueOnce({ id: 71 })
    await click('创建订单并选择付款方式')
    const original = vi.mocked(createOrder).mock.calls[0]
    expect(original).toEqual([1, expect.objectContaining({ targetSubscriptionId: 1, orderType, quoteFingerprint: 'discount-quote', discount: { provider_id: discountProvider.provider_id, selection: 'WELCOME' } })])
    const calls = vi.mocked(previewOrder).mock.calls.length
    wrapper!.unmount(); wrapper = undefined
    await open(`/account/plans?operation=${operation}&subscription=1&plan=${operation === 'change' ? 2 : 1}&step=checkout`)
    expect(createOrder).toHaveBeenCalledOnce()
    expect(vi.mocked(previewOrder).mock.calls).toHaveLength(calls)
    expect(wrapper!.text()).toContain('已确认优惠')
    expect(wrapper!.text()).toContain('原请求目标订阅：#1')
    await click('恢复上次订单')
    expect(vi.mocked(createOrder).mock.calls[1]).toEqual(original)
    expect(paymentAction).not.toHaveBeenCalled()
  })
  it('isolates a late one-order quote when the target subscription changes', async () => {
    vi.mocked(fetchDiscountProviders).mockResolvedValue([{ ...discountProvider, agreement_scope: 'quote_required', operations: ['new', 'upgrade'] }])
    const pending = deferred<OrderPreview>()
    vi.mocked(previewOrder).mockImplementation(async (_sku, _target, options) => options?.discount ? pending.promise : { order_type: 'upgrade', amount_cents: 100, credit_amount: 0, payable_amount: 100, currency: 'CNY', quote_fingerprint: 'baseline', time_credit: 0, traffic_credit: 0, traffic_bytes: 100, used_bytes: 0 })
    await open('/account/plans?operation=change&subscription=1&plan=2&step=checkout')
    await selectDiscount(); await click('核对优惠')
    await router.replace({ query: { ...router.currentRoute.value.query, subscription: '3' } }); await flushPromises()
    const value = discountedQuote(); value.order_type = 'upgrade'; value.discount!.offer.agreement!.scope = 'order_only'; value.discount!.offer.message = 'OLD TARGET OFFER'
    Object.assign(value, { change_mode: 'full_term', credit_amount: 80, settlement_amount: '90', surplus_amount: '10', additional_amount: '0' })
    pending.resolve(value); await flushPromises()
    expect(wrapper!.text()).not.toContain('OLD TARGET OFFER')
    expect(wrapper!.text()).not.toContain('本次优惠仅用于本单')
    expect(createOrder).not.toHaveBeenCalled()
  })

})
