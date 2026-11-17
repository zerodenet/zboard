import { defineComponent, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import UiCheckbox from './UiCheckbox.vue'

describe('UiCheckbox', () => {
  it('allows a controlled checked value to be unchecked and checked again', async () => {
    const wrapper = mount(defineComponent({
      components: { UiCheckbox },
      setup: () => ({ value: ref(true) }),
      template: '<label><UiCheckbox v-model="value" /><span>允许用户续费</span></label>',
    }))
    const control = wrapper.get('[role="checkbox"]')
    await control.trigger('click')
    expect(control.attributes('aria-checked')).toBe('false')
    await control.trigger('click')
    expect(control.attributes('aria-checked')).toBe('true')
    wrapper.unmount()
  })
  it('exposes checked, indeterminate, disabled and accessible label semantics', async () => {
    const wrapper = mount(UiCheckbox, {
      props: { modelValue: false, indeterminate: true, disabled: true },
      attrs: { 'aria-label': '选择当前页节点' },
    })
    const control = wrapper.get('[role="checkbox"]')
    expect(control.attributes('aria-label')).toBe('选择当前页节点')
    expect(control.attributes('disabled')).toBeDefined()
    expect(control.attributes('aria-checked')).toBe('mixed')
    wrapper.unmount()
  })
})
