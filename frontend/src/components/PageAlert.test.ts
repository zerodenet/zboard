import { h } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import PageAlert from './PageAlert.vue'

describe('PageAlert', () => {
  it('shows a repeated error once while preserving inline recovery', () => {
    const wrapper = mount(PageAlert, {
      props: { tone: 'danger', title: '加载失败' },
      slots: { default: () => ['加载失败。 ', h('button', { type: 'button' }, '重试')] },
    })
    expect(wrapper.find('strong').exists()).toBe(false)
    expect(wrapper.text().match(/加载失败/g)).toHaveLength(1)
    expect(wrapper.get('button').text()).toBe('重试')
    expect(wrapper.attributes('role')).toBe('alert')
  })
  it('keeps distinct server details and reacts when the reason changes', async () => {
    const wrapper = mount(PageAlert, { props: { title: '加载失败' }, slots: { default: '加载失败。' } })
    expect(wrapper.find('strong').exists()).toBe(false)
    await wrapper.setProps({ title: '订阅读取失败' })
    expect(wrapper.get('strong').text()).toBe('订阅读取失败')
    expect(wrapper.get('p').text()).toBe('加载失败。')
  })
  it('does not insert an empty description for a title-only alert', () => {
    const wrapper = mount(PageAlert, { props: { title: '服务不可用' } })
    expect(wrapper.get('strong').text()).toBe('服务不可用')
    expect(wrapper.find('p').exists()).toBe(false)
  })
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
