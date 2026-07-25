import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ByteSizeInput from './ByteSizeInput.vue'
import MoneyInput from './MoneyInput.vue'
import MultiplierInput from './MultiplierInput.vue'
import UiNumberInput from './UiNumberInput.vue'

describe('domain number inputs', () => {
  it('keeps FormField accessibility attributes on the real number input', () => {
    const wrapper = mount(UiNumberInput, {
      attrs: { id: 'port', required: true, 'aria-describedby': 'port-message', 'aria-invalid': 'true' },
      props: { modelValue: 443, min: 1, max: 65535 },
      global: { plugins: [PrimeVue] },
    })
    const input = wrapper.get('input')
    expect(input.attributes('id')).toBe('port')
    expect(input.attributes('required')).toBeDefined()
    expect(input.attributes('aria-describedby')).toBe('port-message')
    expect(input.attributes('aria-invalid')).toBe('true')
  })

  it('converts visible currency units to integer cents', async () => {
    const wrapper = mount(MoneyInput, { props: { modelValue: 1234 }, global: { plugins: [PrimeVue] } })
    await wrapper.getComponent(UiNumberInput).vm.$emit('update:modelValue', 56.78)
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([5678])
  })

  it('converts visible GiB to bytes and multiplier units to milli-units', async () => {
    const bytes = mount(ByteSizeInput, { props: { modelValue: 1024 ** 3 }, global: { plugins: [PrimeVue] } })
    await bytes.getComponent(UiNumberInput).vm.$emit('update:modelValue', 1.5)
    expect(bytes.emitted('update:modelValue')?.at(-1)).toEqual([Math.round(1.5 * 1024 ** 3)])

    const multiplier = mount(MultiplierInput, { props: { modelValue: 1000 }, global: { plugins: [PrimeVue] } })
    await multiplier.getComponent(UiNumberInput).vm.$emit('update:modelValue', 1.25)
    expect(multiplier.emitted('update:modelValue')?.at(-1)).toEqual([1250])
  })
})
