import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import WorkbenchFilterSelect from './WorkbenchFilterSelect.vue'

describe('WorkbenchFilterSelect', () => {
  it('renders a Stripe-style add-filter chip and applies a selected value', async () => {
    const wrapper = mount(WorkbenchFilterSelect, {
      props: {
        label: '服务状态',
        modelValue: '',
        options: [
          { label: '全部状态', value: '' },
          { label: '运行中', value: 'active' },
          { label: '已停用', value: 'inactive' },
        ],
      },
      attachTo: document.body,
    })

    await wrapper.get('.workbench-filter-chip-trigger').trigger('click')
    const option = document.body.querySelector<HTMLElement>('[role="option"]')
    expect(option?.textContent).toContain('运行中')
    option?.click()
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['active'])
    expect(wrapper.emitted('apply')).toHaveLength(1)
    wrapper.unmount()
  })

  it('shows an active value and clears it without reopening the popover', async () => {
    const wrapper = mount(WorkbenchFilterSelect, {
      props: {
        label: '服务状态',
        modelValue: 'active',
        options: [
          { label: '全部状态', value: '' },
          { label: '运行中', value: 'active' },
        ],
      },
    })

    expect(wrapper.text()).toContain('运行中')
    await wrapper.get('.workbench-filter-chip-clear').trigger('click')
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([''])
    expect(wrapper.emitted('apply')).toHaveLength(1)
  })

  it('closes the filter popover with Escape and restores trigger focus', async () => {
    const wrapper = mount(WorkbenchFilterSelect, {
      props: {
        label: '协议类型',
        modelValue: '',
        options: [
          { label: '全部协议', value: '' },
          { label: 'VLESS', value: 'vless' },
        ],
      },
      attachTo: document.body,
    })

    const trigger = wrapper.get('.workbench-filter-chip-trigger')
    await trigger.trigger('click')
    expect(trigger.attributes('aria-expanded')).toBe('true')

    const popover = document.body.querySelector<HTMLElement>('.workbench-filter-popover')
    popover?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await wrapper.vm.$nextTick()

    expect(document.body.querySelector('.workbench-filter-popover')).toBeNull()
    expect(document.activeElement).toBe(trigger.element)
    wrapper.unmount()
  })
})
