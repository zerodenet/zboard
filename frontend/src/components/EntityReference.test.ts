import { mount } from '@vue/test-utils'
import { expect, it } from 'vitest'
import EntityReference from './EntityReference.vue'
import TimeBadge from './TimeBadge.vue'

it('does not repeat the primary identity as secondary text', () => {
  const w = mount(EntityReference, { props: { reference: { kind: 'user', id: 4, display_name: 'customer@example.test', secondary: 'customer@example.test' }, compact: true, showId: false } })
  expect(w.text()).toBe('customer@example.test')
  expect(w.find('.entity-meta').exists()).toBe(false)
})
it('retains distinct context and identifiers when requested', () => {
  const w = mount(EntityReference, { props: { reference: { kind: 'user', id: 4, display_name: 'customer@example.test', secondary: '运营联系人' } } })
  expect(w.text()).toContain('运营联系人 · 用户 #4')
})
it('shows missing times quietly and retains precise time for valid values', () => {
  const missing = mount(TimeBadge)
  expect(missing.text()).toBe('-')
  expect(missing.attributes('title')).toBeUndefined()
  const valid = mount(TimeBadge, { props: { value: '2026-11-04T00:00:00Z' } })
  expect(valid.get('time').attributes('datetime')).toBe('2026-11-04T00:00:00.000Z')
  expect(valid.get('time').attributes('title')).toContain('2026')
})
