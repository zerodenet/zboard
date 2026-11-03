import { mount } from '@vue/test-utils'
import { expect, it } from 'vitest'
import BalanceValue from './BalanceValue.vue'
import TimeBadge from './TimeBadge.vue'

it('keeps zero and unknown balances distinct accessibly while showing the same quiet placeholder', async () => {
  const wrapper = mount(BalanceValue, { props: { amount: '0', currency: 'CNY' } })
  expect(wrapper.text()).toBe('-')
  expect(wrapper.attributes('aria-label')).toBe('CNY 0.00')
  await wrapper.setProps({ amount: null })
  expect(wrapper.text()).toBe('-')
  expect(wrapper.attributes('aria-label')).toBe('余额未知')
  await wrapper.setProps({ amount: '123', showCurrency: false })
  expect(wrapper.text()).toBe('1.23')
  expect(wrapper.attributes('aria-label')).toBe('CNY 1.23')
  wrapper.unmount()
})

it('does not decorate absent or invalid dates as real timestamps', async () => {
  const wrapper = mount(TimeBadge)
  expect(wrapper.text()).toBe('-')
  expect(wrapper.find('time, svg').exists()).toBe(false)
  await wrapper.setProps({ value: 'invalid' })
  expect(wrapper.text()).toBe('-')
  expect(wrapper.attributes('title')).toBe('无效时间（invalid）')
  await wrapper.setProps({ value: '2026-11-03T00:00:00Z' })
  expect(wrapper.get('time').attributes('datetime')).toBe('2026-11-03T00:00:00.000Z')
  expect(wrapper.find('svg').exists()).toBe(true)
  wrapper.unmount()
})
