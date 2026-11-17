import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import OverviewCard from './OverviewCard.vue'

describe('OverviewCard', () => {
  it('supports selectable status summaries without changing the card geometry', async () => {
    const wrapper = mount(OverviewCard, {
      props: {
        label: '发布失败',
        value: 3,
        description: '需要立即处理',
        icon: 'alert',
        tone: 'danger',
        interactive: true,
        selected: true,
      },
    })

    const button = wrapper.get('button')
    expect(button.attributes('aria-pressed')).toBe('true')
    expect(wrapper.get('.overview-card-value').text()).toBe('3')
    expect(wrapper.get('.overview-card-description').text()).toBe('需要立即处理')
    await button.trigger('click')
    expect(wrapper.emitted('select')).toHaveLength(1)
  })
})

it('does not reserve a blank description when there is no additional information', () => {
  const w = mount(OverviewCard, { props: { label: '订单', value: 3, icon: 'billing' } })
  expect(w.find('.overview-card-description').exists()).toBe(false)
})
