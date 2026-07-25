import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import UiCheckbox from './UiCheckbox.vue'

describe('UiCheckbox', () => {
  it('exposes checked, indeterminate, disabled and accessible label semantics', async () => {
    const wrapper = mount(UiCheckbox, {
      props: { modelValue: false, indeterminate: true, disabled: true },
      attrs: { 'aria-label': '选择当前页节点' },
      global: { plugins: [PrimeVue] },
    })
    const input = wrapper.get('input[type="checkbox"]')
    expect(input.attributes('aria-label')).toBe('选择当前页节点')
    expect(input.attributes('disabled')).toBeDefined()
    expect((input.element as HTMLInputElement).indeterminate).toBe(true)
  })
})
