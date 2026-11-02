import { mount } from '@vue/test-utils'
import { expect, it } from 'vitest'
import RemoteReadStatus from './RemoteReadStatus.vue'

it('keeps successful reads quiet while retaining refresh, failure and permission feedback', async () => {
  const wrapper = mount(RemoteReadStatus, { props: { label: '节点详情', phase: 'ready', error: '', lastSuccessAt: null } })
  expect(wrapper.find('[role="status"]').exists()).toBe(false)
  await wrapper.setProps({ phase: 'refreshing' })
  expect(wrapper.text()).toContain('更新中，显示上次结果')
  await wrapper.setProps({ phase: 'stale', error: '连接中断' })
  expect(wrapper.text()).toContain('连接中断')
  await wrapper.get('button').trigger('click')
  expect(wrapper.emitted('retry')).toHaveLength(1)
  await wrapper.setProps({ phase: 'forbidden', error: '' })
  expect(wrapper.text()).toContain('已无权查看此内容')
})
