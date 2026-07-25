import { h } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import FormField from './FormField.vue'

describe('FormField', () => {
  it('connects label, hint and validation state to its control', async () => {
    const wrapper = mount(FormField, {
      props: { name: 'ssh-port', label: 'SSH 端口', hint: '范围 1–65535', required: true },
      slots: { default: ({ controlAttrs }: any) => h('input', controlAttrs) },
    })

    const input = wrapper.get('input')
    expect(wrapper.get('label').attributes('for')).toBe('ssh-port')
    expect(input.attributes('id')).toBe('ssh-port')
    expect(input.attributes('required')).toBeDefined()
    expect(input.attributes('aria-describedby')).toBe('ssh-port-message')
    expect(wrapper.get('#ssh-port-message').text()).toBe('范围 1–65535')

    await wrapper.setProps({ error: '端口超出范围' })
    expect(input.attributes('aria-invalid')).toBe('true')
    expect(wrapper.get('#ssh-port-message').attributes('aria-live')).toBe('polite')
    expect(wrapper.get('#ssh-port-message').text()).toContain('端口超出范围')
  })
})
