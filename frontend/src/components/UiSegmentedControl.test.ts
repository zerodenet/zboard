import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import UiSegmentedControl from './UiSegmentedControl.vue'

describe('UiSegmentedControl', () => {
  it('exposes one selected range and emits the chosen period', async () => {
    const wrapper = mount(UiSegmentedControl, {
      props: {
        label: '运营统计周期',
        modelValue: '7d',
        options: [{ label: '今天', value: 'today' }, { label: '近 7 天', value: '7d' }, { label: '近 30 天', value: '30d' }],
      },
      attachTo: document.body,
    })
    const choices = wrapper.findAll('[role="radio"]')
    expect(wrapper.get('[role="radiogroup"]').attributes('aria-label')).toBe('运营统计周期')
    expect(choices[1]?.attributes('aria-checked')).toBe('true')
    await choices[2]!.trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['30d'])
    await wrapper.setProps({ modelValue: '7d' })
    await choices[1]!.trigger('keydown', { key: 'ArrowRight' })
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['30d'])
    wrapper.unmount()
  })
})
