import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import UiTextarea from './UiTextarea.vue'

describe('UiTextarea', () => {
  it('preserves a bound value and emits edits', async () => {
    const wrapper = mount(UiTextarea, {
      props: { modelValue: 'initial value' },
      attrs: { 'aria-label': 'JSON configuration' },
      global: { plugins: [PrimeVue] },
    })

    const textarea = wrapper.get('textarea')
    expect(textarea.element.value).toBe('initial value')
    await textarea.setValue('updated value')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['updated value'])
  })
})
