import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import AnnouncementStack from './AnnouncementStack.vue'

afterEach(() => {
  localStorage.clear()
  document.body.innerHTML = ''
})

describe('AnnouncementStack', () => {
  it('lets a guest acknowledge a non-dismissible notice without blocking the page', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div />' } }] })
    const wrapper = mount(AnnouncementStack, {
      props: {
        items: [{
          id: 7,
          title: '重要维护',
          content: '**今晚升级**',
          severity: 'critical',
          popup_enabled: true,
          dismissible: false,
          revision: 3,
          read: false,
          updated_at: new Date().toISOString(),
        }],
      },
      global: { plugins: [createPinia(), router] },
    })

    expect(wrapper.get('.announcement-dialog').classes()).not.toContain('reader-mode')
    expect(wrapper.find('.announcement-queue').exists()).toBe(false)
    expect(wrapper.find('.announcement-tabs').exists()).toBe(false)
    expect(wrapper.get('.announcement-feature h2').text()).toBe('重要维护')
    const acknowledge = wrapper.get('button.acknowledge')
    expect(acknowledge.text()).toBe('我知道了')
    await acknowledge.trigger('click')
    expect(wrapper.find('.announcement-dialog').exists()).toBe(false)
    expect(localStorage.getItem('zboard.guestAnnouncementReceipts')).toContain('7:3')
  })

  it('opens only for a popup notice and separates other unread notices into a tab', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div />' } }] })
    const now = new Date().toISOString()
    const wrapper = mount(AnnouncementStack, {
      props: { items: [
        { id: 1, title: '重点通知', content: '重点内容', severity: 'warning', popup_enabled: true, dismissible: true, revision: 1, read: false, updated_at: now },
        { id: 2, title: '普通通知', content: '普通内容', severity: 'info', popup_enabled: false, dismissible: true, revision: 1, read: false, updated_at: now },
      ] },
      global: { plugins: [createPinia(), router] },
    })

    expect(wrapper.get('.announcement-feature h2').text()).toBe('重点通知')
    expect(wrapper.get('.announcement-dialog').classes()).toContain('reader-mode')
    expect(wrapper.find('.announcement-tabs').exists()).toBe(true)
    const otherTab = wrapper.findAll('[role="tab"]')[1]
    expect(otherTab.text()).toContain('1')
    await otherTab.trigger('click')
    expect(wrapper.get('.announcement-feature h2').text()).toBe('普通通知')
    expect(wrapper.get('.announcement-dialog').classes()).toContain('reader-mode')
    expect(wrapper.findAll('.announcement-queue button')).toHaveLength(1)
    await wrapper.findAll('[role="tab"]')[0].trigger('click')
    await wrapper.get('button.acknowledge').trigger('click')
    expect(wrapper.get('.announcement-feature h2').text()).toBe('普通通知')
    expect(wrapper.get('.announcement-dialog').classes()).toContain('reader-mode')
    expect(wrapper.findAll('.announcement-queue button')).toHaveLength(1)
  })

  it('keeps the reader and directory when switching from multiple notices to a single-notice category', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div />' } }] })
    const now = new Date().toISOString()
    const wrapper = mount(AnnouncementStack, {
      props: { items: [
        { id: 1, title: '重点通知', content: '重点内容', severity: 'warning', popup_enabled: true, dismissible: true, revision: 1, read: false, updated_at: now },
        { id: 2, title: '另一条重点通知', content: '其他重点内容', severity: 'info', popup_enabled: true, dismissible: true, revision: 1, read: false, updated_at: now },
        { id: 3, title: '普通通知', content: '普通内容', severity: 'info', popup_enabled: false, dismissible: true, revision: 1, read: false, updated_at: now },
      ] },
      global: { plugins: [createPinia(), router] },
    })

    expect(wrapper.get('.announcement-dialog').classes()).toContain('reader-mode')
    expect(wrapper.findAll('.announcement-queue button')).toHaveLength(2)
    expect(wrapper.findAll('.announcement-queue button')[0].attributes('aria-pressed')).toBe('true')
    expect(wrapper.get('.announcement-queue').text()).toContain('另一条重点通知')
    expect(wrapper.get('.announcement-queue').text()).not.toContain('普通通知')
    const firstReadingPane = wrapper.get('.announcement-reading').element
    await wrapper.findAll('.announcement-queue button')[1].trigger('click')
    expect(wrapper.get('.announcement-feature h2').text()).toBe('另一条重点通知')
    expect(wrapper.get('.announcement-reading').element).not.toBe(firstReadingPane)
    expect(wrapper.findAll('.announcement-queue button')[1].attributes('aria-pressed')).toBe('true')
    await wrapper.findAll('[role="tab"]')[1].trigger('click')
    expect(wrapper.get('.announcement-dialog').classes()).toContain('reader-mode')
    expect(wrapper.findAll('.announcement-queue button')).toHaveLength(1)
    expect(wrapper.get('.announcement-queue button').text()).toContain('普通通知')
    expect(wrapper.get('.announcement-queue button').attributes('aria-pressed')).toBe('true')
    await wrapper.findAll('[role="tab"]')[0].trigger('click')
    expect(wrapper.get('.announcement-dialog').classes()).toContain('reader-mode')
    expect(wrapper.findAll('.announcement-queue button')).toHaveLength(2)
  })
})
