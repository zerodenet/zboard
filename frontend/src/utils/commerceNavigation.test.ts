import { describe, expect, it } from 'vitest'
import { accountPurchaseRoute } from './commerceNavigation'

describe('public pricing handoff', () => {
  it('preserves the selected plan, SKU and catalog position for an account detail', () => {
    expect(accountPurchaseRoute({ plan: '3', sku: '7', q: ' trial ', page: '2', limit: '9' })).toEqual({
      path: '/account/plans',
      query: { operation: 'purchase', q: 'trial', page: '2', limit: '9', plan: '3', step: 'detail', sku: '7' },
    })
  })

  it('opens checkout only when a plan and SKU were selected', () => {
    expect(accountPurchaseRoute({ plan: '3', sku: '7' }, 'checkout').query).toEqual({
      operation: 'purchase', plan: '3', step: 'checkout', sku: '7',
    })
    expect(accountPurchaseRoute({ plan: '3' }, 'checkout').query).toEqual({
      operation: 'purchase', plan: '3', step: 'detail',
    })
  })

  it('ignores invalid deep-link identifiers', () => {
    expect(accountPurchaseRoute({ plan: '-1', sku: '7', page: 'nan', limit: '999' }).query).toEqual({
      operation: 'purchase',
    })
  })
})
