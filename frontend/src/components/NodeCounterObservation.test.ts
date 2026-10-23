import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import NodeCounterObservation from './NodeCounterObservation.vue'

const receivedAt = '2026-10-01T10:20:30Z'
const known = {
  known: true,
  latest_source_known: true,
  stats_source_known: true,
  source: 'buffered_stats',
  counter_received_at: receivedAt,
}

describe('node counter observation status', () => {
  it('keeps absent or legacy source unknown without manufacturing a date', () => {
    const wrapper = mount(NodeCounterObservation)
    expect(wrapper.text()).toContain('来源待核对')
    expect(wrapper.text()).toContain('面板接收 时间未知')
    expect(wrapper.find('time').exists()).toBe(false)
  })

  it('displays captured receiver time without claiming online or fresh', () => {
    const wrapper = mount(NodeCounterObservation, { props: { observation: known } })
    expect(wrapper.text()).toContain('来源已记录')
    expect(wrapper.text()).toContain('统计事件')
    expect(wrapper.find('time').attributes('datetime')).toBe('2026-10-01T10:20:30.000Z')
    expect(wrapper.text()).not.toMatch(/在线|新鲜|已应用|已安装/)
  })

  it.each(['latest_source_known', 'stats_source_known', 'known'] as const)('does not promote incomplete %s evidence', field => {
    const wrapper = mount(NodeCounterObservation, { props: { observation: { ...known, [field]: false } } })
    expect(wrapper.text()).toContain('来源待核对')
    expect(wrapper.text()).not.toContain('来源已记录')
  })

  it('keeps retained historical stats distinct from current heartbeat values', async () => {
    const wrapper = mount(NodeCounterObservation, { props: { observation: known } })
    await wrapper.setProps({ observation: { ...known, known: false, source: 'heartbeat' } })
    expect(wrapper.text()).toContain('心跳上报')
    expect(wrapper.text()).toContain('来源待核对')
    expect(wrapper.text()).not.toContain('来源已记录')
    await wrapper.setProps({ observation: { ...known, known: false, source: 'historical_stats' } })
    expect(wrapper.text()).toContain('历史统计')
    expect(wrapper.text()).toContain('来源待核对')
  })

  it.each([null, '', 'invalid-date', '0001-01-01T00:00:00Z'])('does not replace missing receiver time %s with the current time', value => {
    const wrapper = mount(NodeCounterObservation, { props: { observation: { ...known, counter_received_at: value } } })
    expect(wrapper.text()).toContain('时间未知')
    expect(wrapper.find('time').exists()).toBe(false)
  })

  it('does not borrow another position or processing time for the counter', () => {
    const wrapper = mount(NodeCounterObservation, { props: { observation: {
      ...known,
      counter_received_at: null,
      latest_received_at: receivedAt,
      stats_received_at: receivedAt,
      stats_occurred_at: '2026-09-01T00:00:00Z',
    } } })
    expect(wrapper.text()).toContain('时间未知')
    expect(wrapper.find('time').exists()).toBe(false)
  })

  it('handles an unfamiliar source conservatively and stays read-only', () => {
    const wrapper = mount(NodeCounterObservation, { props: { observation: { ...known, source: 'future_source' } } })
    expect(wrapper.text()).toContain('来源待核对')
    expect(wrapper.text()).toContain('来源未确认')
    expect(wrapper.findAll('button')).toHaveLength(0)
    expect(wrapper.emitted()).toEqual({})
  })
})
