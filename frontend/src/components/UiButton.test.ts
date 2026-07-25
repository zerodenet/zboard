import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import UiButton from './UiButton.vue'

describe('UiButton', () => {
  it('uses explicit variants and prevents duplicate submission while loading', () => {
    const wrapper = mount(UiButton, {
      props: { variant: 'danger', size: 'sm', loading: true },
      attrs: { type: 'submit', form: 'test-form' },
      slots: { default: '删除节点' },
      global: { plugins: [PrimeVue] },
    })
    const button = wrapper.get('button')
    expect(button.attributes('disabled')).toBeDefined()
    expect(button.attributes('aria-busy')).toBe('true')
    expect(button.attributes('type')).toBe('submit')
    expect(button.attributes('form')).toBe('test-form')
    expect(button.classes()).toContain('p-button-danger')
    expect(button.classes()).toContain('p-button-sm')
  })
})
