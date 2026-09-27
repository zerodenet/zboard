import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import UiButton from './UiButton.vue'

describe('UiButton', () => {
  it('uses explicit variants and prevents duplicate submission while loading', () => {
    const wrapper = mount(UiButton, {
      props: { variant: 'danger', size: 'sm', loading: true },
      attrs: { type: 'submit', form: 'test-form' },
      slots: { default: '删除节点' },
    })
    const button = wrapper.get('button')
    expect(button.attributes('disabled')).toBeDefined()
    expect(button.attributes('aria-busy')).toBe('true')
    expect(button.attributes('type')).toBe('submit')
    expect(button.attributes('form')).toBe('test-form')
    expect(button.attributes('data-slot')).toBe('button')
    expect(button.attributes('data-variant')).toBe('destructive')
    expect(button.classes()).toContain('ui-button-destructive')
    expect(button.classes()).toContain('ui-button-sm')
  })

  it('replaces an icon-only button with a single loading indicator', async () => {
    const wrapper = mount(UiButton, {
      props: { icon: true, loading: true },
      slots: { default: '<svg class="original-icon" aria-hidden="true" />' },
    })
    expect(wrapper.find('.ui-button-spinner').exists()).toBe(true)
    expect(wrapper.find('.original-icon').exists()).toBe(false)
    await wrapper.setProps({ loading: false })
    expect(wrapper.find('.ui-button-spinner').exists()).toBe(false)
    expect(wrapper.find('.original-icon').exists()).toBe(true)
  })

  it('retains the label on a loading button with an icon', () => {
    const wrapper = mount(UiButton, {
      props: { loading: true },
      slots: { default: '<svg class="ui-icon" aria-hidden="true" />保存配置' },
    })
    expect(wrapper.get('button').text()).toBe('保存配置')
    expect(wrapper.find('.ui-button-spinner').exists()).toBe(true)
  })
})
