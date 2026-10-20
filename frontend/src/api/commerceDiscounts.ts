import { authenticatedAPI, previewOrder, type OrderPreview } from './client'

export interface DiscountProvider {
  provider_id: string
  name: string
  description: string
  operation: 'new'
  agreement_scope: 'initial_only'
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
    || row.operation !== 'new' || row.agreement_scope !== 'initial_only')) {
    throw new Error('优惠列表读取失败，请稍后重试。')
  }
  return rows
}

export async function previewDiscountOrder(skuId: number, currency: string, discount: OrderDiscountSelection, signal?: AbortSignal): Promise<OrderPreview> {
  if (new TextEncoder().encode(discount.selection || '').length > 256) throw new Error('优惠码不能超过 256 字节。')
  const quote = await previewOrder(skuId, 0, { discount, signal })
  const offer = quote?.discount?.offer
  if (!offer || quote.order_type !== 'new' || quote.discount?.provider?.plugin_id !== discount.provider_id
    || offer.status !== 'available' || offer.agreement?.scope !== 'initial_only'
    || quote.currency !== currency || offer.currency !== currency
    || !Number.isSafeInteger(quote.base_price_minor) || quote.base_price_minor! < 0
    || !Number.isSafeInteger(quote.discount_amount) || quote.discount_amount! < 0
    || quote.discount_amount! > quote.base_price_minor! || quote.amount_cents !== quote.base_price_minor
    || offer.discount_minor !== quote.discount_amount || quote.payable_amount !== quote.base_price_minor! - quote.discount_amount!
    || typeof quote.quote_fingerprint !== 'string' || !quote.quote_fingerprint
    || !Number.isFinite(Date.parse(offer.expires_at)) || Date.parse(offer.expires_at) <= Date.now()) {
    throw new Error('优惠报价不可确认，请重新核对优惠或选择不使用优惠。')
  }
  return quote
}
