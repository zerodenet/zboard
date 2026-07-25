import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import PageAlert from './PageAlert.vue'

describe('PageAlert', () => {
  it('keeps a persistent error and exposes its recovery action', () => {
    const wrapper = mount(PageAlert, {
      props: { tone: 'danger', title: '加载失败' },
      slots: {
        default: '无法读取详情。',
        actions: '<button type="button">重试</button>',
      },
      global: {
        stubs: {
          UiIcon: true,
          UiButton: true,
        },
      },
    })

    expect(wrapper.attributes('role')).toBe('alert')
    expect(wrapper.attributes('aria-atomic')).toBe('true')
    expect(wrapper.text()).toContain('加载失败')
    expect(wrapper.get('.page-alert-actions button').text()).toBe('重试')
  })
})
