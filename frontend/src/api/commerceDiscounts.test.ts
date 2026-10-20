import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fetchDiscountProviders, previewDiscountOrder, validateDiscountOrderQuote } from './commerceDiscounts'
import { createOrder, type OrderPreview } from './client'

const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('axios', () => ({ default: { create: () => ({ get, post,
  interceptors: { request: { use: vi.fn() }, response: { use: vi.fn() } },
}) } }))
const provider = { provider_id: 'native.offer', name: '会员优惠', description: '本次新购优惠', operation: 'new', agreement_scope: 'initial_only' }
const discount = { provider_id: provider.provider_id, selection: 'WELCOME' }
function quote() {
  return { order_type: 'new', base_price_minor: 100, amount_cents: 100, discount_amount: 20, payable_amount: 80, currency: 'CNY', quote_fingerprint: 'confirmed-quote',
    discount: { provider: { plugin_id: provider.provider_id }, offer: { status: 'available', currency: 'CNY', discount_minor: 20, agreement: { scope: 'initial_only' }, expires_at: '2099-01-01T00:00:00Z' } } }
}
describe('purchase discount API boundaries', () => {
  beforeEach(() => { get.mockReset(); post.mockReset() })
  it('reads authenticated choices with cancellation and preserves an empty list', async () => {
    const signal = new AbortController().signal
    get.mockResolvedValueOnce({ data: { data: [provider] } }).mockResolvedValueOnce({ data: { data: [] } })
    expect(await fetchDiscountProviders(signal)).toEqual([provider])
    expect(get).toHaveBeenLastCalledWith('/commerce/discount-providers', { signal })
    expect(await fetchDiscountProviders()).toEqual([])
  })
  it.each([null, {}, [{ ...provider, operation: 'renewal' }], [{ ...provider, agreement_scope: 'recurring' }], [{ ...provider, name: '' }]])('rejects malformed discovery rather than guessing a usable provider', async value => {
    get.mockResolvedValue({ data: { data: value } })
    await expect(fetchDiscountProviders()).rejects.toThrow('优惠列表读取失败')
  })
  it('accepts quote-required discovery without claiming its eventual agreement scope', async () => {
    const choice = { ...provider, agreement_scope: 'quote_required' }
    get.mockResolvedValue({ data: { data: [choice] } })
    expect(await fetchDiscountProviders()).toEqual([choice])
  })
  it('accepts an available recurring initial purchase only after actual preview', async () => {
    const value = quote(); value.discount.offer.agreement.scope = 'recurring'
    post.mockResolvedValue({ data: { data: value } })
    expect(await previewDiscountOrder(3, 'CNY', discount)).toBe(value)
  })
  it('validates inherited renewal facts independently of catalog price and discovery', () => {
    const value = quote(); value.order_type = 'renewal'; value.discount.offer.agreement.scope = 'recurring'
    expect(validateDiscountOrderQuote(value as OrderPreview, { orderType: 'renewal' })).toBe(value)
    value.discount.offer.agreement.scope = 'initial_only'
    expect(() => validateDiscountOrderQuote(value as OrderPreview, { orderType: 'renewal' })).toThrow('继承的优惠报价不可确认')
  })
  it('sends references only in preview and final fingerprint plus references in creation', async () => {
    const signal = new AbortController().signal, value = quote()
    post.mockResolvedValueOnce({ data: { data: value } }).mockResolvedValueOnce({ data: { data: { id: 7 } } })
    expect(await previewDiscountOrder(3, 'CNY', discount, signal)).toBe(value)
    expect(post).toHaveBeenNthCalledWith(1, '/orders/preview', { plan_sku_id: 3, target_subscription_id: 0, discount }, { signal })
    await createOrder(3, { discount, quoteFingerprint: value.quote_fingerprint, idempotencyKey: 'intent' })
    expect(post).toHaveBeenNthCalledWith(2, '/orders', { plan_sku_id: 3, channel: 'manual', order_type: 'new', target_subscription_id: undefined, discount, quote_fingerprint: 'confirmed-quote', idempotency_key: 'intent' })
  })
  it.each(['unavailable', 'requires_confirmation'])('does not accept %s as a purchasable quote', async status => {
    const value = quote(); value.discount.offer.status = status
    post.mockResolvedValue({ data: { data: value } })
    await expect(previewDiscountOrder(3, 'CNY', discount)).rejects.toThrow('优惠报价不可确认')
  })
  it.each(['provider', 'scope', 'currency', 'price', 'discount', 'expiry', 'fingerprint'])('rejects an inconsistent or stale %s', async field => {
    const value = quote()
    if (field === 'provider') value.discount.provider.plugin_id = 'another'
    if (field === 'scope') value.discount.offer.agreement.scope = 'unknown'
    if (field === 'currency') value.currency = 'USD'
    if (field === 'price') value.base_price_minor = 101
    if (field === 'discount') value.discount_amount = 200
    if (field === 'expiry') value.discount.offer.expires_at = '2000-01-01T00:00:00Z'
    if (field === 'fingerprint') value.quote_fingerprint = ''
    post.mockResolvedValue({ data: { data: value } })
    await expect(previewDiscountOrder(3, 'CNY', discount)).rejects.toThrow('优惠报价不可确认')
  })
  it('rejects oversized codes before previewing and propagates actual provider failures', async () => {
    await expect(previewDiscountOrder(3, 'CNY', { ...discount, selection: '中'.repeat(86) })).rejects.toThrow('256')
    expect(post).not.toHaveBeenCalled()
    const cause = { response: { status: 503, data: { error: { version: 1, code: 'discount_unavailable' } } } }
    post.mockRejectedValue(cause)
    await expect(previewDiscountOrder(3, 'CNY', discount)).rejects.toBe(cause)
  })
})
