import { authenticatedAPI as api } from './client'
import type { DiscountQuote } from './commerceDiscounts'

export interface AutomaticRenewalOffer {
  plan_sku_id: number
  currency: string
  base_price_minor: string
  billing_unit: string
  billing_value: number
  quote_fingerprint: string
  agreement_version?: 1 | 2
  discount_amount_minor?: string
  payable_amount_minor?: string
  discount?: DiscountQuote
}
export interface AutomaticRenewalView {
  subscription_id: number
  enabled: boolean
  plan_sku_id: number
  revision: number
  state: string
  message: string
  last_order_id?: number
  next_attempt_at?: string
  offer?: AutomaticRenewalOffer
}
export interface AutomaticRenewalRequest {
  enabled: boolean
  plan_sku_id: number
  quote_fingerprint: string
  expected_revision: number
}
export async function fetchAutomaticRenewal(id: number, sku?: number): Promise<AutomaticRenewalView> {
  const { data } = await api.get(`/subscriptions/${id}/automatic-renewal`, { params: sku ? { plan_sku_id: sku } : undefined })
  return data.data
}
export async function setAutomaticRenewal(id: number, request: AutomaticRenewalRequest): Promise<AutomaticRenewalView> {
  const { data } = await api.put(`/subscriptions/${id}/automatic-renewal`, request)
  return data.data
}
