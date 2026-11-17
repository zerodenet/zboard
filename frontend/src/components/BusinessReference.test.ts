import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import BusinessReference from './BusinessReference.vue'
afterEach(() => vi.restoreAllMocks())
it('copies the canonical reference with accessible success feedback', async () => {
  const write = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
  const wrapper = mount(BusinessReference, { props: { value: 'DD0000000048', label: '订单编号' } })
  await wrapper.get('button').trigger('click'); await flushPromises()
  expect(write).toHaveBeenCalledWith('DD0000000048')
  expect(wrapper.get('[role="status"]').text()).toBe('订单编号已复制')
  wrapper.unmount()
})
it('keeps the value selectable when clipboard access fails', async () => {
  vi.spyOn(navigator.clipboard, 'writeText').mockRejectedValue(new Error('denied'))
  const wrapper = mount(BusinessReference, { props: { value: 'TK0000000012', label: '工单编号' } })
  await wrapper.get('button').trigger('click'); await flushPromises()
  expect(wrapper.get('code').text()).toBe('TK0000000012')
  expect(wrapper.get('[role="alert"]').text()).toContain('复制失败')
  await wrapper.setProps({ value: 'TK0000000013' })
  expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  wrapper.unmount()
})
