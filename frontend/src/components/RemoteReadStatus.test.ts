import { flushPromises, mount } from '@vue/test-utils'
import { expect, it } from 'vitest'
import RemoteReadStatus from './RemoteReadStatus.vue'

it('keeps compact refresh metadata stable and preserves a readable failure until recovery', async () => {
  const wrapper = mount(RemoteReadStatus, { attachTo: document.body, props: { compact: true, label: '经营统计', phase: 'ready', error: '', lastSuccessAt: '2026-11-02T00:00:00Z' } })
  const root = wrapper.get('.remote-read-status-compact').element, text = wrapper.text()
  await wrapper.setProps({ phase: 'refreshing' })
  expect(wrapper.get('.remote-read-status-compact').element).toBe(root)
  expect(wrapper.text()).toBe(text)
  await wrapper.setProps({ phase: 'stale', error: '数据库响应超时，请稍后再试' })
  await wrapper.get('[aria-label="经营统计：查看失败原因"]').trigger('click'); await flushPromises()
  expect(document.body.textContent).toContain('数据库响应超时，请稍后再试')
  const scope = root.getAttributeNames().find(name => name.startsWith('data-v-'))!
  expect(scope).toBeTruthy()
  expect(document.querySelector('.read-error-details')?.hasAttribute(scope)).toBe(true)
  const retry = [...document.body.querySelectorAll('button')].find(b => b.textContent === '重试')!
  retry.click(); await flushPromises(); expect(wrapper.emitted('retry')).toHaveLength(1)
  await wrapper.setProps({ phase: 'refreshing', error: '' })
  expect(wrapper.text()).toContain('经营统计：更新失败')
  await wrapper.setProps({ phase: 'ready' })
  expect(wrapper.text()).not.toContain('更新失败')
  expect(wrapper.get('.remote-read-status-compact').element).toBe(root)
  wrapper.unmount()
})

it('keeps successful reads quiet while retaining refresh, failure and permission feedback', async () => {
  const wrapper = mount(RemoteReadStatus, { props: { label: '节点详情', phase: 'ready', error: '', lastSuccessAt: null } })
  expect(wrapper.find('[role="status"]').exists()).toBe(false)
  await wrapper.setProps({ phase: 'refreshing' })
  expect(wrapper.text()).toContain('更新中，显示上次结果')
  expect(wrapper.find('.remote-read-status').exists()).toBe(false)
  expect(wrapper.get('[role="status"]').classes()).toContain('sr-only')
  await wrapper.setProps({ phase: 'stale', error: '连接中断' })
  expect(wrapper.text()).toContain('连接中断')
  await wrapper.get('button').trigger('click')
  expect(wrapper.emitted('retry')).toHaveLength(1)
  await wrapper.setProps({ phase: 'forbidden', error: '' })
  expect(wrapper.text()).toContain('已无权查看此内容')
})
