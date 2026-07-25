import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const sourceRoot = join(import.meta.dirname, '..')

function source(...parts: string[]) {
  return readFileSync(join(sourceRoot, ...parts), 'utf8')
}

describe('self-service catalog scale policy', () => {
  it('keeps the public pricing catalog bounded and uses one primary SKU', () => {
    const pricing = source('views', 'PublicPlans.vue')

    expect(pricing).toContain('fetchPlanCatalogPage')
    expect(pricing).toContain('useRemoteTable')
    expect(pricing).toContain('<TablePager')
    expect(pricing).toContain('plan.primary_sku')
    expect(pricing).not.toMatch(/\bfetchPlans\(/)
    expect(pricing).not.toContain('plan.skus')
  })

  it('loads account plans and the selected plan SKU page independently', () => {
    const plans = source('views', 'account', 'AccountPlans.vue')

    expect(plans).toContain('fetchPlanCatalogPage')
    expect(plans).toContain('fetchPlanCatalogItem')
    expect(plans).toContain('fetchPlanCatalogSKUs')
    expect(plans).toContain('useRemoteTable<PlanCatalogItem>')
    expect(plans).toContain('useRemoteTable<PlanSKU>')
    expect(plans).toContain(':total="skuTotal"')
    expect(plans).not.toMatch(/\bfetchPlans\(/)
    expect(plans).not.toContain('plan.skus')
  })

  it('searches a bounded active template page for account exports', () => {
    const subscriptions = source('views', 'account', 'AccountSubscription.vue')

    expect(subscriptions).toContain('fetchActiveSubscriptionTemplatesPage')
    expect(subscriptions).toContain('limit: 25')
    expect(subscriptions).toContain('searchTemplates')
    expect(subscriptions).not.toMatch(/\bfetchSubscriptionTemplates\(/)
  })
})
