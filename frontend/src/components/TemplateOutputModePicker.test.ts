import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import TemplateOutputModePicker from './TemplateOutputModePicker.vue'

describe('TemplateOutputModePicker', () => {
  it('presents all backend-owned renderers as accessible choices', async () => {
    const wrapper = mount(TemplateOutputModePicker, {
      props: { modelValue: 'clash' },
      global: { plugins: [PrimeVue] },
    })

    const options = wrapper.findAll('[role="radio"]')
    expect(options).toHaveLength(6)
    expect(options.map(option => option.text())).toEqual([
      expect.stringContaining('Zero'),
      expect.stringContaining('Clash'),
      expect.stringContaining('sing-box'),
      expect.stringContaining('Shadowrocket'),
      expect.stringContaining('Quantumult X'),
      expect.stringContaining('v2rayN'),
    ])
    expect(options[1].attributes('aria-checked')).toBe('true')

    await options[5].trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['v2rayn'])
  })
})
