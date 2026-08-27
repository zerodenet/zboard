import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

function readSource(relativePath: string) {
  return readFileSync(fileURLToPath(new URL(relativePath, import.meta.url)), 'utf8')
}

describe('commerce purchase experience source contract', () => {
  it('keeps the homepage preview static while recommendations use live catalog data', () => {
    const source = readSource('./Home.vue')
    expect(source).toContain('fetchPlanCatalogPage')
    expect(source).toContain('CommercePlanCard')
    expect(source).toContain('storefront-home-plans')
    expect(source).toContain('套餐选购流程介绍')
    expect(source).toContain('找到适合你的服务方案')
    expect(source).toContain('浏览全部套餐')
    expect(source).not.toContain('featuredPlan')
    expect(source).not.toContain('formatCurrency')
    expect(source).not.toContain('72.6')
    expect(source).not.toContain('2026/08/06')
    expect(source).not.toContain('个人标准版')
  })

  it('keeps the public catalog compact and moves specifications into product detail', () => {
    const source = readSource('./PublicPlans.vue')
    expect(source).toContain('storefront-catalog-toolbar')
    expect(source).toContain('CommercePlanCard')
    expect(source).toContain('CommercePlanDetail')
    expect(source).toContain("fetchPlanCatalogSKUs(planID, { operation: 'purchase', offset: 0, limit: 100 })")
    expect(source).not.toContain('commerce-offer-tabs')
    expect(source).not.toContain('skuPage')
    expect(source).not.toContain('skuPager')
    expect(source).not.toContain('暂未定价')
  })

  it('starts subscription operations from a target and keeps order errors in checkout', () => {
    const source = readSource('./account/AccountPlans.vue')
    expect(source).toContain("startOperation('renew', subscription.id)")
    expect(source).toContain("startOperation('change', subscription.id)")
    expect(source).toContain("startOperation('addon', subscription.id)")
    expect(source).toContain('CommercePlanDetail')
    expect(source).toContain('purchase-checkout__section')
    expect(source).toContain('订单确认')
    expect(source).toContain('确认创建订单')
    expect(source).toContain('checkoutError')
    expect(source).toContain('commerceErrorMessage')
    expect(source).toContain('无法创建订单')
    expect(source.indexOf('订单确认')).toBeLessThan(source.indexOf('确认创建订单'))
    expect(source).not.toContain('ModalDialog')
    expect(source).not.toContain('commerce-offer-tabs')
  })

  it('keeps administrator confirmation errors inside the confirmation dialog', () => {
    const source = readSource('./Orders.vue')
    expect(source).toContain(':error="actionError"')
    expect(source).toContain('commerceErrorMessage')
    expect(source).toContain('actionError.value = commerceErrorMessage')
    expect(source).not.toContain("catch (e: any) { error.value = e?.response?.data?.message")
  })

  it('does not ship prototype annotations as customer-facing copy', () => {
    const sources = [
      readSource('./Home.vue'),
      readSource('./PublicPlans.vue'),
      readSource('./account/AccountPlans.vue'),
    ].join('\n')
    for (const annotation of ['当前仓库能力', '目标能力', '接口依赖', '推荐方案', '原型说明', '验收重点']) {
      expect(sources).not.toContain(annotation)
    }
  })
})
