import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import MetricComparison from './MetricComparison.vue'
describe('metric comparison', () => {
  it.each([[0,0,'持平'],[5,0,'上期为 0'],[15,10,'↑ 50.0%'],[5,10,'↓ 50.0%']])('compares %d with %d without invalid percentages', (current, previous, expected) => {
    const view = mount(MetricComparison, { props: { current, previous, label: '对比昨日同期' } })
    expect(view.text()).toContain(expected)
    expect(view.text()).toContain('对比昨日同期')
    expect(view.text()).not.toMatch(/Infinity|NaN/)
    view.unmount()
  })
})
