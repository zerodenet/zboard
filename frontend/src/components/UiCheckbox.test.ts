import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import UiCheckbox from './UiCheckbox.vue'

describe('UiCheckbox', () => {
  it('exposes checked, indeterminate, disabled and accessible label semantics', async () => {
    const wrapper = mount(UiCheckbox, {
      props: { modelValue: false, indeterminate: true, disabled: true },
      attrs: { 'aria-label': '选择当前页节点' },
    })
    const control = wrapper.get('[role="checkbox"]')
    expect(control.attributes('aria-label')).toBe('选择当前页节点')
    expect(control.attributes('disabled')).toBeDefined()
    expect(control.attributes('aria-checked')).toBe('mixed')
  })
})
