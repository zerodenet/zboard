import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import MetricCard from './MetricCard.vue'

describe('MetricCard', () => {
  it('keeps status semantic and allows formatted metadata components', () => {
    const wrapper = mount(MetricCard, {
      props: {
        label: '今日使用',
        value: '12.5 GiB',
        icon: 'clock',
        status: '今日',
      },
      slots: {
        meta: '<time datetime="2026-07-24T00:00:00.000Z">刚刚</time>',
      },
    })

    expect(wrapper.get('.status-badge').text()).toContain('今日')
    expect(wrapper.get('.metric-value').text()).toBe('12.5 GiB')
    expect(wrapper.get('.metric-meta time').attributes('datetime')).toBe('2026-07-24T00:00:00.000Z')
  })
})
