import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const sourceRoot = join(process.cwd(), 'src')
const read = (...parts: string[]) => readFileSync(join(sourceRoot, ...parts), 'utf8').replace(/\r\n/g, '\n')

describe('commerce SKU model', () => {
  it('keeps billing cadence independent from entitlement fulfillment and operations', () => {
    const plans = read('views', 'Plans.vue')

    expect(plans).toContain("entitlement_mode: 'plan'")
    expect(plans).toContain("sku.entitlement_mode === 'traffic_addon'")
    expect(plans).toContain("sku.billing_mode === 'one_time' ? `一次性付费")
    expect(plans).toContain("if (sku.billing_unit === 'once') return '永久有效 · 流量用完为止'")
    expect(plans).toContain("if (sku.billing_mode === 'periodic' && sku.billing_unit === 'once') sku.billing_unit = 'month'")
    expect(plans).not.toContain("if (sku.billing_unit === 'once') sku.billing_unit = 'month'")
    expect(plans).not.toContain("if (sku.billing_mode === 'one_time') {\n    sku.billing_unit = 'once'")
    expect(plans).not.toContain("sku.allowed_operations = ['addon']\n    sku.sku_type = 'traffic_pack'\n    return\n  }\n  sku.billing_mode")
  })

  it('shows addon traffic only for an explicit traffic addon and announces modal failures globally', () => {
    const plans = read('views', 'Plans.vue')

    expect(plans).toContain("form.sku.entitlement_mode === 'traffic_addon'")
    expect(plans).toContain("skuDraft.entitlement_mode === 'traffic_addon'")
    expect(plans).toContain("notify('无法创建商品'")
    expect(plans).toContain("notify('无法保存规格'")
    expect(plans).not.toContain('v-if="createErrors.formError.value"')
    expect(plans).not.toContain('v-if="skuErrors.formError.value"')
  })

  it('queries the storefront by allowed operation instead of the legacy SKU type', () => {
    const client = read('api', 'client.ts')
    const accountPlans = read('views', 'account', 'AccountPlans.vue')

    expect(client).toContain("query.set('operation', params.operation)")
    expect(accountPlans).toContain('operation: operation.value')
    expect(accountPlans).not.toContain('skuTypeByOperation')
  })

  it('models renewal fulfillment explicitly for timed and permanent SKUs', () => {
    const plans = read('views', 'Plans.vue')
    const client = read('api', 'client.ts')

    expect(client).toContain("renewal_effect: 'none' | 'extend_only' | 'extend_and_add_quota' | 'add_quota_only'")
    expect(plans).toContain('再次购买效果')
    expect(plans).toContain("value: 'extend_only'")
    expect(plans).toContain("value: 'extend_and_add_quota'")
    expect(plans).toContain("value: 'add_quota_only'")
    expect(plans).toContain("} else if (sku.billing_unit === 'once') {")
    expect(plans).toContain("sku.renewal_effect = 'add_quota_only'")
  })

  it('lets exhausted permanent subscriptions enter the quota replenishment flow', () => {
    const accountPlans = read('views', 'account', 'AccountPlans.vue')
    const detail = read('components', 'CommercePlanDetail.vue')
    const orders = read('views', 'Orders.vue')

    expect(accountPlans).toContain("status: 'expired'")
    expect(accountPlans).toContain('isPermanentSubscription')
    expect(accountPlans).toContain("? '补充额度' : '续费'")
    expect(accountPlans).toContain("add_quota_only: '只补充一份套餐流量，永久有效期不变'")
    expect(detail).toContain('再次购买效果')
    expect(orders).toContain('orderTypeName(selectedOrder.order_type, selectedOrder.renewal_effect)')
    expect(orders).toContain('补充额度')
  })
})
