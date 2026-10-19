import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import CommercePlanDetail from './CommercePlanDetail.vue'

vi.mock('../stores/app', () => ({ useAppStore: () => ({ siteProfile: { policyDocuments: [] } }) }))

const props = {
  plan: { id: 1, name: 'Original plan', slug: 'original', traffic_bytes: 100, speed_limit_mbps: 0, device_limit: 1 },
  skus: [{ id: 1, name: 'Monthly', price_cents: 20000, currency: 'CNY', billing_unit: 'month', billing_value: 1, renewal_effect: 'extend_and_add_quota' }],
  mode: 'renew', selectedSkuId: 1,
} as any

describe('renewal confirmation', () => {
  it('waits for the locked server quote and describes the current renewal contract', async () => {
    const wrapper = mount(CommercePlanDetail, { props })
    expect(wrapper.get('.storefront-order-summary__total').text()).toContain('正在核算')
    const next = wrapper.findAll('button').at(-1)!
    expect(next.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('保留当前用量和流量重置日期')
    expect(wrapper.text()).not.toContain('并增加一份套餐流量')
    await wrapper.setProps({ quote: { payable_amount: 12000, currency: 'CNY' } as any })
    expect(wrapper.get('.storefront-order-summary__total').text()).toContain('120.00')
    expect(next.attributes('disabled')).toBeUndefined()
    await next.trigger('click')
    expect(wrapper.emitted('continue')).toHaveLength(1)
    await wrapper.setProps({ renewalRecovering: true })
    expect(wrapper.text()).toContain('恢复服务，并开始新的套餐周期和流量额度')
    wrapper.unmount()
  })
})
