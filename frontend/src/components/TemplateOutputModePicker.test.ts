import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import TemplateOutputModePicker from './TemplateOutputModePicker.vue'

describe('TemplateOutputModePicker', () => {
  it('presents only backend-owned renderers as accessible choices', async () => {
    const wrapper = mount(TemplateOutputModePicker, {
      props: { modelValue: 'clash' },
      global: { plugins: [PrimeVue] },
    })

    const options = wrapper.findAll('[role="radio"]')
    expect(options).toHaveLength(3)
    expect(options[1].attributes('aria-checked')).toBe('true')

    await options[2].trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['sing-box'])
  })
})
