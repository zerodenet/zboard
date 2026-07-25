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
