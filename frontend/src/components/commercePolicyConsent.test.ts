import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const source = readFileSync(join(import.meta.dirname, 'CommercePlanDetail.vue'), 'utf8')

describe('purchase policy consent', () => {
  it('uses dynamically placed documents and requires explicit acknowledgement', () => {
    expect(source).toContain("policyDocumentsFor(profile.value, 'purchase')")
    expect(source).toContain('我已阅读并同意以上与本次购买相关的服务规则')
    expect(source).toContain('hasPurchasePolicies && !purchasePoliciesAccepted')
    expect(source).toContain('font-size: 12px')
    expect(source).not.toContain('font-size: 9px')
  })
})
