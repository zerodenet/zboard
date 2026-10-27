import type { AutomaticRenewalOffer } from './automaticRenewal'

export class AutomaticRenewalOfferError extends Error {}

function minor(value: unknown): bigint {
  if (typeof value !== 'string' || !/^\d{1,19}$/.test(value)) throw new AutomaticRenewalOfferError('自动续费金额无法核对，请重新读取报价。')
  const amount = BigInt(value)
  if (amount > 9223372036854775807n) throw new AutomaticRenewalOfferError('自动续费金额超出可核对范围。')
  return amount
}

// Host-owned decimal strings are the displayed monetary facts. The browser
// returns only the server fingerprint/revision, never any amount or agreement.
export function checkedAutomaticRenewalOffer(value: AutomaticRenewalOffer): AutomaticRenewalOffer {
  if (!value || !Number.isSafeInteger(value.plan_sku_id) || value.plan_sku_id <= 0 || !/^[A-Z]{3}$/.test(value.currency)
    || !['day', 'month', 'year'].includes(value.billing_unit) || !Number.isSafeInteger(value.billing_value) || value.billing_value <= 0
    || typeof value.quote_fingerprint !== 'string' || !/^[a-f0-9]{64}$/.test(value.quote_fingerprint)) {
    throw new AutomaticRenewalOfferError('自动续费报价无法核对，请重新读取。')
  }
  const base = minor(value.base_price_minor)
  if (value.agreement_version === undefined || value.agreement_version === 1) {
    if (value.discount || (value.discount_amount_minor !== undefined && minor(value.discount_amount_minor) !== 0n)
      || (value.payable_amount_minor !== undefined && minor(value.payable_amount_minor) !== base)) {
      throw new AutomaticRenewalOfferError('优惠授权版本无法核对，不会按基础价开启自动扣款。')
    }
    return value
  }
  if (value.agreement_version !== 2) throw new AutomaticRenewalOfferError('优惠授权版本无法核对。')
  const reduction = minor(value.discount_amount_minor), payable = minor(value.payable_amount_minor)
  const quote = value.discount, offer = quote?.offer
  if (reduction > base || payable !== base - reduction || !offer || (offer.redemption !== undefined && offer.redemption !== 'exclusive_until_core_finalization_v1') || offer.status !== 'available' || offer.agreement?.scope !== 'recurring'
    || typeof quote?.provider?.plugin_id !== 'string' || !quote.provider.plugin_id.trim() || offer.currency !== value.currency
    || !Number.isFinite(Date.parse(offer.expires_at)) || Date.parse(offer.expires_at) <= Date.now()) {
    throw new AutomaticRenewalOfferError('长期优惠自动续费报价无法确认，请重新核对原优惠约定。')
  }
  return value
}
