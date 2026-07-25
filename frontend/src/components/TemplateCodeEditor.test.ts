import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import TemplateCodeEditor from './TemplateCodeEditor.vue'

describe('TemplateCodeEditor', () => {
  it('renders line numbers, marks a diagnostic line and inserts spaces for Tab', async () => {
    const wrapper = mount(TemplateCodeEditor, {
      props: { modelValue: 'one\ntwo\nthree', errorLine: 2 },
      global: { plugins: [PrimeVue] },
    })

    const lines = wrapper.findAll('.template-code-gutter span')
    expect(lines.map(line => line.text())).toEqual(['1', '2', '3'])
    expect(lines[1].classes()).toContain('invalid')

    const textarea = wrapper.get('textarea')
    ;(textarea.element as HTMLTextAreaElement).setSelectionRange(3, 3)
    await textarea.trigger('keydown', { key: 'Tab' })
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['one  \ntwo\nthree'])
  })
})
