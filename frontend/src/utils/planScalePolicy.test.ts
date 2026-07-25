import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const sourceRoot = join(import.meta.dirname, '..')

describe('plan SKU scale policy', () => {
  it('keeps SKU collections out of the plan detail contract', () => {
    const client = readFileSync(join(sourceRoot, 'api', 'client.ts'), 'utf8')
    const detailContract = client.match(/export interface PlanDetail[\s\S]*?\n}\n/)?.[0] || ''

    expect(detailContract).not.toContain('skus:')
    expect(client).toContain('fetchPlanSKUs')
    expect(client).toContain('fetchPlanSKU')
  })

  it('uses an independently paginated SKU workbench and ID lookup', () => {
    const plans = readFileSync(join(sourceRoot, 'views', 'Plans.vue'), 'utf8')

    expect(plans).toContain('useRemoteTable<PlanSKU>')
    expect(plans).toContain('fetchPlanSKUs')
    expect(plans).toContain('fetchPlanSKU')
    expect(plans).toContain(':total="skuTotal"')
    expect(plans).toContain(':offset="skuOffset"')
    expect(plans).not.toContain('detailPlan.skus')
  })
})
