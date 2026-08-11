import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { PlanCatalogItem } from '../api/client'
import CommercePlanCard from './CommercePlanCard.vue'

const plan: PlanCatalogItem = {
  id: 11,
  name: 'Starter',
  slug: 'starter',
  summary: '入门套餐',
  node_group_id: 1,
  is_active: true,
  sort_order: 0,
  revision: 3,
  sku_count: 2,
  active_sku_count: 0,
  created_at: '2026-08-11T00:00:00Z',
  updated_at: '2026-08-11T00:00:00Z',
  traffic_bytes: 107374182400,
  speed_limit_mbps: 100,
  device_limit: 5,
}

function mountCard(props: Record<string, unknown> = {}) {
  return mount(CommercePlanCard, {
    props: { plan, ...props },
    global: {
      stubs: {
        UiButton: {
          props: ['disabled'],
          emits: ['click'],
          template: '<button type="button" :disabled="disabled" @click="$emit(\'click\')"><slot /></button>',
        },
        UiIcon: { template: '<span />' },
      },
    },
  })
}

describe('CommercePlanCard', () => {
  it('keeps plan entitlements inspectable when no purchasable SKU exists', async () => {
    const wrapper = mountCard({ offer: null, offerCount: 0, disabled: true })

    expect(wrapper.text()).toContain('暂不可购买')
    expect(wrapper.text()).toContain('100 GB 套餐流量')
    expect(wrapper.text()).toContain('100 Mbps')
    expect(wrapper.text()).toContain('5 台设备')

    const button = wrapper.get('button')
    expect(button.attributes('disabled')).toBeUndefined()
    await button.trigger('click')
    expect(wrapper.emitted('select')).toHaveLength(1)
  })

  it('still blocks navigation while offer state is loading', () => {
    const wrapper = mountCard({ offer: null, disabled: true, loading: true })
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
  })
})
