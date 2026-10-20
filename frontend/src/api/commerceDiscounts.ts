import { authenticatedAPI, previewOrder, type OrderPreview } from './client'

export interface DiscountProvider {
  provider_id: string
  name: string
  description: string
  operation: 'new'
  agreement_scope: 'initial_only' | 'quote_required'
}

export interface OrderDiscountSelection {
  provider_id: string
  selection?: string
}

export interface DiscountQuote {
  provider: { plugin_id: string; signing_key_fingerprint: string; package_digest: string; generation: number; config_revision: number }
  offer: {
    status: 'available' | 'unavailable' | 'requires_confirmation'
    input_fingerprint: string
    currency: string
    discount_minor: number
    agreement?: { id: string; version: string; scope: 'initial_only' | 'recurring'; terms_hash: string; terms_json: string }
    expires_at: string
    message?: string
  }
}

export async function fetchDiscountProviders(signal?: AbortSignal): Promise<DiscountProvider[]> {
  const response = await authenticatedAPI.get('/commerce/discount-providers', { signal })
  const rows = response.data?.data
  if (!Array.isArray(rows) || rows.some(row => !row || typeof row.provider_id !== 'string' || !row.provider_id.trim()
    || typeof row.name !== 'string' || !row.name.trim() || typeof row.description !== 'string'
    || row.operation !== 'new' || !['initial_only', 'quote_required'].includes(row.agreement_scope))) {
    throw new Error('优惠列表读取失败，请稍后重试。')
  }
  return rows
}

export async function previewDiscountOrder(skuId: number, currency: string, discount: OrderDiscountSelection, signal?: AbortSignal): Promise<OrderPreview> {
  if (new TextEncoder().encode(discount.selection || '').length > 256) throw new Error('优惠码不能超过 256 字节。')
  const quote = await previewOrder(skuId, 0, { discount, signal })
  return validateDiscountOrderQuote(quote, { orderType: 'new', currency, providerId: discount.provider_id })
}

// Renewal ownership, provider selection and price sources remain server-owned.
// A catalog price or discovery hint never substitutes for this actual quote.
export function validateDiscountOrderQuote(quote: OrderPreview, expected: { orderType: 'new' | 'renewal'; currency?: string; providerId?: string }): OrderPreview {
  const offer = quote?.discount?.offer
  const scope = offer?.agreement?.scope
  if (!offer || quote.order_type !== expected.orderType || typeof quote.discount?.provider?.plugin_id !== 'string' || !quote.discount.provider.plugin_id.trim()
    || (expected.providerId !== undefined && quote.discount.provider.plugin_id !== expected.providerId)
    || offer.status !== 'available' || !scope || !['initial_only', 'recurring'].includes(scope)
    || (expected.orderType === 'renewal' && scope !== 'recurring')
    || typeof quote.currency !== 'string' || !/^[A-Z]{3}$/.test(quote.currency)
    || (expected.currency !== undefined && quote.currency !== expected.currency) || offer.currency !== quote.currency
    || !Number.isSafeInteger(quote.base_price_minor) || quote.base_price_minor! < 0
    || !Number.isSafeInteger(quote.discount_amount) || quote.discount_amount! < 0
    || quote.discount_amount! > quote.base_price_minor! || quote.amount_cents !== quote.base_price_minor
    || offer.discount_minor !== quote.discount_amount || quote.payable_amount !== quote.base_price_minor! - quote.discount_amount!
    || typeof quote.quote_fingerprint !== 'string' || !quote.quote_fingerprint
    || !Number.isFinite(Date.parse(offer.expires_at)) || Date.parse(offer.expires_at) <= Date.now()) {
    throw new Error(expected.orderType === 'renewal' ? '继承的优惠报价不可确认，请重新核对原优惠约定。' : '优惠报价不可确认，请重新核对优惠。')
  }
  return quote
}
