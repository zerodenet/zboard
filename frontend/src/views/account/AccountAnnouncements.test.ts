import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { afterEach, expect, it, vi } from 'vitest'
import AccountAnnouncements from './AccountAnnouncements.vue'
import UiSection from '../../components/UiSection.vue'
import UiButton from '../../components/UiButton.vue'
import PageRefreshButton from '../../components/PageRefreshButton.vue'
const api = vi.hoisted(() => ({ list: vi.fn(), read: vi.fn() }))
vi.mock('../../api/client', async original => ({ ...await original<typeof import('../../api/client')>(), fetchAccountAnnouncements: api.list, markAnnouncementRead: api.read }))
let wrapper: VueWrapper | undefined
afterEach(() => { wrapper?.unmount(); vi.clearAllMocks() })
async function render() {
  api.list.mockResolvedValue({ items: [
    { id: 1, revision: 1, title: '维护通知', content: '本周维护窗口调整', active: true, read: true, severity: 'info', created_at: '2026-11-03T00:00:00Z' },
    { id: 2, revision: 1, title: '客户端更新', content: '本次更新改善连接稳定性', active: false, read: true, severity: 'info', created_at: '2026-11-02T00:00:00Z' },
  ], total: 2, unread_count: 0 })
  wrapper = mount(AccountAnnouncements, { global: { plugins: [createPinia()], components: { UiSection, UiButton, PageRefreshButton } } })
  await flushPromises(); return wrapper
}
it('uses the historical section heading once and expands the announcement body', async () => {
  const w = await render(), trigger = w.get('.announcement-heading')
  expect(w.text().match(/历史公告/g)).toHaveLength(1)
  expect(trigger.text()).not.toContain('历史')
  expect(trigger.text()).not.toContain('已结束')
  expect(w.find('.announcement-body').exists()).toBe(false)
  await trigger.trigger('click'); await flushPromises()
  expect(trigger.attributes('aria-expanded')).toBe('true')
  expect(w.get('.announcement-body').text()).toContain('本次更新改善连接稳定性')
  expect(api.read).not.toHaveBeenCalled()
})
it('retains confirmed announcements after a failed refresh without replacing them with an empty state', async () => {
  const w = await render()
  api.list.mockRejectedValueOnce(new Error('offline'))
  await w.get('button[aria-label="刷新公告"]').trigger('click'); await flushPromises()
  expect(w.text()).toContain('维护通知')
  expect(w.text()).toContain('客户端更新')
  expect(w.text()).not.toContain('暂无公告')
})

it('keeps announcement bodies collapsed until opened and omits redundant read-state labels', async () => {
  const w = await render(), trigger = w.get('.featured-heading')
  expect(trigger.attributes('aria-expanded')).toBe('false')
  expect(trigger.text()).not.toContain('已读')
  expect(w.find('.featured-body').exists()).toBe(false)
  await trigger.trigger('click'); await flushPromises()
  expect(w.get('.featured-body').text()).toContain('本周维护窗口调整')
  expect(api.read).not.toHaveBeenCalled()
})
