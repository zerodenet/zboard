import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import AccountCleanupSummary from './AccountCleanupSummary.vue'
import type { AccountCleanupStatus } from '../api/client'

const status: AccountCleanupStatus = { enabled: true, inactive_days: 14, state: 'scheduled', eligible_at: '2026-11-04T00:00:00Z', reason: '无有效订阅' }
beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(new Date('2026-11-03T00:00:00Z')) })
afterEach(() => { vi.useRealTimers() })

it('shows the deadline without repeating the policy and turns red when that deadline arrives', async () => {
  const wrapper = mount(AccountCleanupSummary, { props: { status } })
  expect(wrapper.find('small').exists()).toBe(false)
  expect(wrapper.get('time').attributes('datetime')).toBe(status.eligible_at)
  expect(wrapper.classes()).not.toContain('account-cleanup-due')
  await vi.advanceTimersByTimeAsync(24 * 60 * 60_000)
  expect(wrapper.classes()).toContain('account-cleanup-due')
  expect(wrapper.get('time').attributes('aria-label')).toContain('已到清理时间')
  wrapper.unmount()
  expect(vi.getTimerCount()).toBe(0)
})

it('marks an eligible account red without inventing an in-progress deletion', () => {
  const wrapper = mount(AccountCleanupSummary, { props: { status: { ...status, state: 'eligible', eligible_at: null } } })
  expect(wrapper.classes()).toContain('account-cleanup-due')
  expect(wrapper.text()).toBe('-')
  expect(wrapper.get('span').attributes('aria-label')).toContain('待清理')
  wrapper.unmount()
})

it.each(['protected', 'disabled'] as const)('does not mark %s accounts overdue even if an old deadline remains', state => {
  const wrapper = mount(AccountCleanupSummary, { props: { status: { ...status, state, enabled: state !== 'disabled', eligible_at: '2020-01-01T00:00:00Z', reason: '管理员账号受保护' } } })
  expect(wrapper.classes()).not.toContain('account-cleanup-due')
  expect(wrapper.find('time').exists()).toBe(false)
  expect(wrapper.find('small').exists()).toBe(false)
  expect(wrapper.text()).toBe('-')
  wrapper.unmount()
})

it('does not invent a date for permanent subscriptions or invalid deadlines', async () => {
  const wrapper = mount(AccountCleanupSummary, { props: { status: { ...status, state: 'subscribed', eligible_at: null } } })
  expect(wrapper.text()).toBe('-')
  await wrapper.setProps({ status: { ...status, eligible_at: 'invalid' } })
  expect(wrapper.text()).toBe('-')
  expect(wrapper.classes()).not.toContain('account-cleanup-due')
  await wrapper.setProps({ status: null })
  expect(wrapper.text()).toBe('-')
  wrapper.unmount()
})
