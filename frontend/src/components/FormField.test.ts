import { h } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import FormField from './FormField.vue'

describe('FormField', () => {
  it('does not reserve visible help space until a hint or error is present', async () => {
    const wrapper = mount(FormField, { props: { name: 'quiet-field', label: '名称' } })
    expect(wrapper.classes()).toContain('form-field-quiet')
    expect(wrapper.get('#quiet-field-message').classes()).toContain('sr-only')
    await wrapper.setProps({ error: '请填写名称' })
    expect(wrapper.classes()).not.toContain('form-field-quiet')
    expect(wrapper.get('#quiet-field-message').classes()).not.toContain('sr-only')
    expect(wrapper.get('#quiet-field-message').attributes('aria-live')).toBe('polite')
    wrapper.unmount()
  })
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
