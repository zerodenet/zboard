import { mount } from '@vue/test-utils'
import { expect, it } from 'vitest'
import RuntimeOperationsOverview from './RuntimeOperationsOverview.vue'

it('does not show unknown spool counts as a healthy empty queue', async () => {
  const spool = { pending_events: 0, pending_bytes: 0, pressure_level: 'normal', pending_unknown: true, read_error: '事件段 12 元数据读取失败' }
  const wrapper = mount(RuntimeOperationsOverview, { props: { data: { queues: [], runtime: { event_spool: spool } }, executionUnavailable: false, diagnosticsUnavailable: false } })
  try {
    expect(wrapper.get('.spool-line').text()).toContain(spool.read_error)
    expect(wrapper.find('.spool-line strong').exists()).toBe(false)
    await wrapper.setProps({ data: { queues: [], runtime: { event_spool: { ...spool, pending_unknown: false, read_error: '' } } } })
    expect(wrapper.get('.spool-line strong').text()).toContain('0')
  } finally { wrapper.unmount() }
})
