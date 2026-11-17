import { describe, expect, it } from 'vitest'
import { checkedAutomaticRenewalOffer } from './automaticRenewalOffer'
import type { AutomaticRenewalOffer } from './automaticRenewal'

function recurring(): AutomaticRenewalOffer {
  return { plan_sku_id: 3, currency: 'CNY', billing_unit: 'year', billing_value: 1, base_price_minor: '12000', discount_amount_minor: '2000', payable_amount_minor: '10000', agreement_version: 2, quote_fingerprint: 'a'.repeat(64),
    discount: { provider: { plugin_id: 'native.quote', signing_key_fingerprint: 'hidden', package_digest: 'hidden', generation: 1, config_revision: 1 },
      offer: { status: 'available', currency: 'CNY', discount_minor: 2000, input_fingerprint: 'hidden', expires_at: '2099-01-01T00:00:00Z', agreement: { id: 'ongoing', version: '1', scope: 'recurring', terms_hash: 'hidden', terms_json: '{"opaque":true}' } } } }
}

describe('automatic renewal approval quotes', () => {
  it('keeps verified host string amounts exact above JavaScript integer precision', () => {
    const value = recurring()
    value.base_price_minor = '9007199254740993'; value.discount_amount_minor = '2000'; value.payable_amount_minor = '9007199254738993'
    expect(checkedAutomaticRenewalOffer(value)).toBe(value)
    const { discount, discount_amount_minor, payable_amount_minor, agreement_version, ...legacy } = value
    expect(checkedAutomaticRenewalOffer(legacy)).toBe(legacy)
  })
  it.each(['missing-price', 'wrong-total', 'negative', 'overflow', 'unknown-version', 'unavailable', 'confirmation', 'scope', 'currency', 'expired', 'provider', 'fingerprint'])( 'refuses %s without reconstructing an undiscounted approval', field => {
    const value = recurring()
    if (field === 'missing-price') delete value.payable_amount_minor
    if (field === 'wrong-total') value.payable_amount_minor = '12000'
    if (field === 'negative') value.discount_amount_minor = '-1'
    if (field === 'overflow') value.base_price_minor = '9223372036854775808'
    if (field === 'unknown-version') (value as any).agreement_version = 3
    if (field === 'unavailable') value.discount!.offer.status = 'unavailable'
    if (field === 'confirmation') value.discount!.offer.status = 'requires_confirmation'
    if (field === 'scope') value.discount!.offer.agreement!.scope = 'initial_only'
    if (field === 'currency') value.discount!.offer.currency = 'USD'
    if (field === 'expired') value.discount!.offer.expires_at = '2000-01-01T00:00:00Z'
    if (field === 'provider') value.discount!.provider.plugin_id = ''
    if (field === 'fingerprint') value.quote_fingerprint = 'not-an-approval'
    expect(() => checkedAutomaticRenewalOffer(value)).toThrow()
  })
  it('requires a recognized explicit reservation obligation before displaying debit consent', () => {
    const value = recurring(); value.discount!.offer.redemption = 'exclusive_until_core_finalization_v1'
    expect(checkedAutomaticRenewalOffer(value)).toBe(value)
    ;(value.discount!.offer as any).redemption = 'provider_expires_hold_without_core'
    expect(() => checkedAutomaticRenewalOffer(value)).toThrow('无法确认')
  })
  it('never relabels discounted evidence as a legacy full-price offer', () => {
    const value = recurring(); delete value.agreement_version
    expect(() => checkedAutomaticRenewalOffer(value)).toThrow('不会按基础价')
  })
})
