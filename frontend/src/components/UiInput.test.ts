import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import UiInput from './UiInput.vue'

describe('UiInput', () => {
  it('forwards native input semantics and ARIA attributes', () => {
    const wrapper = mount(UiInput, {
      attrs: {
        type: 'email',
        value: 'operator@example.com',
        autocomplete: 'email',
        inputmode: 'email',
        readonly: true,
        'aria-label': '管理员邮箱',
      },
      global: { plugins: [PrimeVue] },
    })

    const input = wrapper.get('input')
    expect(input.attributes('type')).toBe('email')
    expect(input.attributes('value')).toBe('operator@example.com')
    expect(input.attributes('autocomplete')).toBe('email')
    expect(input.attributes('inputmode')).toBe('email')
    expect(input.attributes('readonly')).toBeDefined()
    expect(input.attributes('aria-label')).toBe('管理员邮箱')
  })

  it('preserves number model semantics', async () => {
    const wrapper = mount(UiInput, {
      props: { modelValue: 4, modelModifiers: { number: true } } as any,
      attrs: { type: 'number', min: '1', max: '9', step: '1' },
      global: { plugins: [PrimeVue] },
    })
    await wrapper.get('input').setValue('7')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([7])
  })
})
