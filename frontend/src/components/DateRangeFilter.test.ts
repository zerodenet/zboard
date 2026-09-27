import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import DateRangeFilter from './DateRangeFilter.vue'

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-09-24T08:00:00Z'))
})
afterEach(() => vi.useRealTimers())

describe('DateRangeFilter', () => {
  it('offers UTC presets with future ranges for subscription expiry', async () => {
    const wrapper = mount(DateRangeFilter, {
      props: { label: '到期日期', from: '', to: '', presetDirection: 'future' },
    })
    expect(wrapper.attributes('aria-label')).toBe('到期日期（UTC）')
    expect(wrapper.findAll('.date-range-month')).toHaveLength(2)
    await wrapper.get('.date-range-presets button:nth-child(2)').trigger('click')
    expect(wrapper.emitted('update:from')?.at(-1)).toEqual(['2026-09-24'])
    expect(wrapper.emitted('update:to')?.at(-1)).toEqual(['2026-09-30'])
    await wrapper.get('.date-range-presets button:last-child').trigger('click')
    expect(wrapper.emitted('update:from')?.at(-1)).toEqual([''])
    expect(wrapper.emitted('update:to')?.at(-1)).toEqual([''])
    wrapper.unmount()
  })

  it('selects a calendar range and keeps the inclusive end visible', async () => {
    const wrapper = mount(DateRangeFilter, {
      props: { label: '记录日期', from: '', to: '' },
    })
    await wrapper.get('button[aria-label="2026年9月25日"]:not(:disabled)').trigger('click')
    expect(wrapper.emitted('update:from')?.at(-1)).toEqual(['2026-09-25'])
    expect(wrapper.emitted('update:to')?.at(-1)).toEqual(['2026-09-25'])
    await wrapper.setProps({ from: '2026-09-25', to: '2026-09-25' })
    await wrapper.get('button[aria-label="2026年9月20日"]:not(:disabled)').trigger('click')
    expect(wrapper.emitted('update:from')?.at(-1)).toEqual(['2026-09-20'])
    expect(wrapper.emitted('update:to')?.at(-1)).toEqual(['2026-09-25'])
    await wrapper.setProps({ from: '2026-09-20', to: '2026-09-25' })
    expect(wrapper.text()).toContain('2026-09-20  →  2026-09-25')
    expect(wrapper.text()).toContain('含结束日')
    wrapper.unmount()
  })

  it('warns when an existing range exceeds the backend limit', () => {
    const wrapper = mount(DateRangeFilter, {
      props: { label: '记录日期', from: '2025-01-01', to: '2026-09-24' },
    })
    expect(wrapper.get('[role="alert"]').text()).toContain('366 天')
    wrapper.unmount()
  })
})
