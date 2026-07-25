import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import CursorPager from './CursorPager.vue'
import UiSelect from './UiSelect.vue'

describe('CursorPager', () => {
  it('labels cursor directions and only enables available navigation', async () => {
    const wrapper = mount(CursorPager, {
      props: { count: 50, total: 120, limit: 50, hasPrevious: false, hasNext: true },
      global: { plugins: [PrimeVue] },
    })
    const buttons = wrapper.findAll('button')
    expect(buttons[0].attributes('aria-label')).toBe('较新记录')
    expect(buttons[0].attributes('disabled')).toBeDefined()
    expect(buttons[1].attributes('aria-label')).toBe('较旧记录')
    expect(buttons[1].attributes('disabled')).toBeUndefined()
    expect(wrapper.findAll('.cursor-pager-nav .ui-icon')).toHaveLength(2)
    await buttons[1].trigger('click')
    expect(wrapper.emitted('next')).toHaveLength(1)
    wrapper.getComponent(UiSelect).vm.$emit('update:modelValue', 100)
    expect(wrapper.emitted('limit')).toEqual([[100]])
    expect(wrapper.getComponent(UiSelect).props('options')).toEqual([
      { label: '25', value: 25 },
      { label: '50', value: 50 },
      { label: '100', value: 100 },
    ])
  })
})
