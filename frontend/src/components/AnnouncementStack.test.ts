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

    expect(wrapper.get('.announcement-dialog').classes()).not.toContain('has-queue')
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

    expect(wrapper.get('.announcement-feature h3').text()).toBe('重点通知')
    expect(wrapper.get('.announcement-dialog').classes()).toContain('has-queue')
    const otherTab = wrapper.findAll('[role="tab"]')[1]
    expect(otherTab.text()).toContain('1')
    await otherTab.trigger('click')
    expect(wrapper.get('.announcement-feature h3').text()).toBe('普通通知')
  })
})
