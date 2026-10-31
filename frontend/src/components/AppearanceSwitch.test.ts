import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, expect, it } from 'vitest'
import AppearanceSwitch from './AppearanceSwitch.vue'
import { useAppearance, appearanceStorageKey } from '../theme/appearance'

afterEach(() => { useAppearance().setPreference('system'); localStorage.removeItem(appearanceStorageKey) })

it('offers light, dark and system as accessible radio choices and switches immediately', async () => {
  const wrapper = mount(AppearanceSwitch, { attachTo: document.body })
  const trigger = wrapper.get('button')
  await trigger.trigger('click', { button: 0, ctrlKey: false })
  await flushPromises()
  const items = Array.from(document.querySelectorAll<HTMLElement>('[role="menuitemradio"]'))
  expect(items.map(item => item.textContent?.trim())).toEqual(['浅色', '深色', '跟随系统'])
  items[1].click()
  await flushPromises()
  expect(trigger.attributes('aria-label')).toBe('外观模式：深色')
  expect(document.documentElement.classList.contains('dark')).toBe(true)
  expect(localStorage.getItem(appearanceStorageKey)).toBe('dark')
  wrapper.unmount()
})
