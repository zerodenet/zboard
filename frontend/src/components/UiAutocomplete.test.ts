import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import UiAutocomplete from './UiAutocomplete.vue'

describe('UiAutocomplete', () => {
  it('displays a selection loaded after the input is mounted', async () => {
    const wrapper = mount(UiAutocomplete, { props: { modelValue: null, optionLabel: 'name', forceSelection: true } })
    await flushPromises()
    await wrapper.setProps({ modelValue: { id: 1, name: '边缘网络' } })
    await flushPromises()
    expect(wrapper.get<HTMLInputElement>('input').element.value).toBe('边缘网络')
    expect(wrapper.emitted('clear')).toBeUndefined()
    expect(wrapper.emitted('complete')).toBeUndefined()
    wrapper.unmount()
  })
  it('keeps the field id on its actual combobox input', () => {
    const wrapper = mount(UiAutocomplete, { attrs: { id: 'plan-node-group', 'aria-describedby': 'group-hint' } })
    expect(wrapper.get('input').attributes('id')).toBe('plan-node-group')
    expect(wrapper.get('input').attributes('aria-describedby')).toBe('group-hint')
    wrapper.unmount()
  })
  it('clears unselected text when a forced selection closes', async () => {
    const wrapper = mount(UiAutocomplete, { props: { forceSelection: true } })
    await wrapper.get('input').setValue('unknown node')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['unknown node'])
    await wrapper.get('input').trigger('keydown', { key: 'Escape' })
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([null])
    expect(wrapper.emitted('clear')).toHaveLength(1)
    wrapper.unmount()
  })
})
