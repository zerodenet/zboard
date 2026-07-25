import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import DateRangeFilter from './DateRangeFilter.vue'
import UiDateInput from './UiDateInput.vue'

describe('DateRangeFilter', () => {
  it('keeps visible UTC context and emits bounded date values', async () => {
    const wrapper = mount(DateRangeFilter, {
      props: { label: '创建日期', from: '2026-07-01', to: '2026-07-03' },
      global: { plugins: [PrimeVue] },
    })
    expect(wrapper.attributes('role')).toBe('group')
    expect(wrapper.text()).toContain('创建日期')
    expect(wrapper.text()).toContain('UTC')
    const inputs = wrapper.findAllComponents(UiDateInput)
    expect(inputs[0].attributes('max')).toBe('2026-07-03')
    expect(inputs[1].attributes('min')).toBe('2026-07-01')
    await inputs[0].vm.$emit('update:modelValue', '2026-07-02')
    await inputs[1].vm.$emit('update:modelValue', '2026-07-04')
    expect(wrapper.emitted('update:from')?.at(-1)).toEqual(['2026-07-02'])
    expect(wrapper.emitted('update:to')?.at(-1)).toEqual(['2026-07-04'])
  })
})
