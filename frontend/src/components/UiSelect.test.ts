import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import UiSelect from './UiSelect.vue'

afterEach(() => { document.body.innerHTML = '' })

describe('UiSelect', () => {
  it('keeps empty values selectable and exposes a labelled combobox', async () => {
    const wrapper = mount(UiSelect, {
      attachTo: document.body,
      props: {
        modelValue: '',
        options: [
          { label: '全部状态', value: '' },
          { label: '正常', value: 'active' },
          { label: '已停用', value: 'disabled', disabled: true },
        ],
      },
      attrs: { 'aria-label': '账户状态' },
    })
    const trigger = wrapper.get('[role="combobox"]')
    expect(trigger.attributes('aria-label')).toBe('账户状态')
    expect(trigger.text()).toContain('全部状态')
    await trigger.trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    const items = Array.from(document.body.querySelectorAll<HTMLElement>('[role="option"]'))
    expect(items.map(item => item.textContent?.trim().replace(/✓/g, ''))).toEqual(['全部状态', '正常', '已停用'])
    expect(items[2].getAttribute('data-disabled')).not.toBeNull()
    items[1].dispatchEvent(new PointerEvent('pointerup', { bubbles: true, pointerType: 'mouse' }))
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['active'])
    expect(wrapper.emitted('change')?.at(-1)?.[0]).toMatchObject({ value: 'active', target: { value: 'active' } })
    wrapper.unmount()
  })

  it('renders grouped options inside the shared menu surface', async () => {
    const wrapper = mount(UiSelect, {
      attachTo: document.body,
      props: { options: [{ label: '运行状态', options: [{ label: '正常', value: 'active' }] }] },
    })
    await wrapper.get('[role="combobox"]').trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    expect(document.body.querySelector('.ui-select-group-label')?.textContent).toBe('运行状态')
    expect(document.body.querySelector('.ui-select-content')).not.toBeNull()
    wrapper.unmount()
  })
})
