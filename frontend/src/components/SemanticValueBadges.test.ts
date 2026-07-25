import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import StatusBadge from './StatusBadge.vue'
import TimeBadge from './TimeBadge.vue'
import UiIcon from './UiIcon.vue'

describe('semantic value badges', () => {
  it('always pairs a status label with a semantic icon', () => {
    const wrapper = mount(StatusBadge, {
      props: { tone: 'success' },
      slots: { default: '运行中' },
    })

    expect(wrapper.text()).toContain('运行中')
    expect(wrapper.getComponent(UiIcon).props('name')).toBe('check')
    expect(wrapper.attributes('data-tone')).toBe('success')
  })

  it('renders machine-readable and human-formatted time instead of raw output', () => {
    const wrapper = mount(TimeBadge, {
      props: { value: '2026-07-23T04:05:06Z', mode: 'exact' },
    })
    const time = wrapper.get('time')

    expect(time.attributes('datetime')).toBe('2026-07-23T04:05:06.000Z')
    expect(time.attributes('title')).toContain('2026')
    expect(time.attributes('aria-label')).toContain('精确时间')
    expect(time.text()).not.toBe('2026-07-23T04:05:06Z')
    expect(wrapper.getComponent(UiIcon).props('name')).toBe('clock')
  })
})
