import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

function readSource(relativePath: string) {
  return readFileSync(fileURLToPath(new URL(relativePath, import.meta.url)), 'utf8')
}

describe('commerce purchase experience source contract', () => {
  it('keeps the public pricing page product-focused', () => {
    const source = readSource('./PublicPlans.vue')
    expect(source).toContain('commerce-catalog-grid')
    expect(source).toContain('commerce-offer-tabs')
    expect(source).toContain('fetchPlanCatalogSKUs')
    expect(source).not.toContain('DataWorkbench')
    expect(source).not.toContain('WorkbenchFilterBar')
    expect(source).not.toContain('暂未定价')
    expect(source).not.toContain('active_sku_count')
  })

  it('starts renewal, plan changes and add-ons from a subscription context', () => {
    const source = readSource('./account/AccountPlans.vue')
    expect(source).toContain("startOperation('renew', subscription.id)")
    expect(source).toContain("startOperation('change', subscription.id)")
    expect(source).toContain("startOperation('addon', subscription.id)")
    expect(source).toContain('确认目标订阅')
    expect(source).not.toContain('commerce-mode-tabs')
    expect(source).not.toContain('这次要做什么？')
  })
})
