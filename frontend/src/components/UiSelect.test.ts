import PrimeVue from 'primevue/config'
import PrimeSelect from 'primevue/select'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import UiSelect from './UiSelect.vue'

describe('UiSelect', () => {
  it('accepts structured options without parsing option VNodes', async () => {
    const wrapper = mount(UiSelect, {
      props: {
        modelValue: '',
        options: [
          { label: '全部状态', value: '' },
          { label: '正常', value: 'active' },
          { label: '已停用', value: 'disabled', disabled: true },
        ],
      },
      attrs: { 'aria-label': '账户状态' },
      global: { plugins: [PrimeVue] },
    })

    expect(wrapper.get('[role="combobox"]').attributes('aria-label')).toBe('账户状态')
    const select = wrapper.findComponent(PrimeSelect)
    expect(select.props('options')).toEqual([
      { label: '全部状态', value: '' },
      { label: '正常', value: 'active' },
      { label: '已停用', value: 'disabled', disabled: true },
    ])
    expect(select.props('optionGroupLabel')).toBeUndefined()
    expect(select.props('optionGroupChildren')).toBeUndefined()
    expect(select.props('modelValue')).toBe('')
    select.vm.$emit('update:modelValue', 'active')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['active'])
  })

  it('only enables PrimeVue option grouping for grouped options', () => {
    const wrapper = mount(UiSelect, {
      props: {
        options: [
          {
            label: '运行状态',
            options: [
              { label: '正常', value: 'active' },
              { label: '已停用', value: 'disabled' },
            ],
          },
        ],
      },
      global: { plugins: [PrimeVue] },
    })

    const select = wrapper.findComponent(PrimeSelect)
    expect(select.props('optionGroupLabel')).toBe('label')
    expect(select.props('optionGroupChildren')).toBe('options')
  })
})
