import { mount } from '@vue/test-utils'
import PrimeVue from 'primevue/config'
import { describe, expect, it } from 'vitest'
import WorkbenchFilterInput from './WorkbenchFilterInput.vue'

describe('WorkbenchFilterInput', () => {
  it('keeps text in the popover draft until the user applies it', async () => {
    const wrapper = mount(WorkbenchFilterInput, {
      props: {
        label: '搜索',
        modelValue: '',
        placeholder: '订阅 ID、邮箱、套餐或 SKU',
      },
      attachTo: document.body,
      global: { plugins: [PrimeVue] },
    })

    await wrapper.get('.workbench-filter-chip-trigger').trigger('click')
    const input = document.body.querySelector<HTMLInputElement>('.workbench-filter-popover input')
    expect(input).not.toBeNull()
    input!.value = '  example@example.com  '
    input!.dispatchEvent(new Event('input', { bubbles: true }))
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('update:modelValue')).toBeUndefined()

    const apply = document.body.querySelector<HTMLButtonElement>('.workbench-filter-form > button')
    apply?.click()
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['example@example.com'])
    expect(wrapper.emitted('apply')).toHaveLength(1)
    wrapper.unmount()
  })

  it('renders the committed query as an active chip and clears it directly', async () => {
    const wrapper = mount(WorkbenchFilterInput, {
      props: {
        label: '搜索',
        modelValue: 'active query',
      },
      global: { plugins: [PrimeVue] },
    })

    expect(wrapper.text()).toContain('active query')
    await wrapper.get('.workbench-filter-chip-clear').trigger('click')
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([''])
    expect(wrapper.emitted('apply')).toHaveLength(1)
  })
})
