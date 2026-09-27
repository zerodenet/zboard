import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import UiAutocomplete from './UiAutocomplete.vue'

describe('UiAutocomplete', () => {
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
