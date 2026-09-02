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
          dismissible: false,
          revision: 3,
          read: false,
          updated_at: new Date().toISOString(),
        }],
      },
      global: { plugins: [createPinia(), router] },
    })

    const acknowledge = wrapper.get('button.acknowledge')
    expect(acknowledge.text()).toBe('我知道了')
    await acknowledge.trigger('click')
    expect(wrapper.find('.announcement-bar').exists()).toBe(false)
    expect(localStorage.getItem('zboard.guestAnnouncementReceipts')).toContain('7:3')
  })
})
