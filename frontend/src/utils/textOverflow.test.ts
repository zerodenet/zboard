import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import { vOverflowTitle } from '../directives/overflowTitle'

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })
it('adds a title only while content is clipped and disconnects when removed', async () => {
  let clipped = false
  const callbacks: Array<() => void> = [], disconnect = vi.fn()
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(100)
  vi.spyOn(HTMLElement.prototype, 'scrollWidth', 'get').mockImplementation(() => clipped ? 200 : 100)
  vi.stubGlobal('ResizeObserver', class { constructor(callback: () => void) { callbacks.push(callback) } observe() {} disconnect = disconnect })
  const fixture = defineComponent({ props: ['value'], setup: () => ({ vOverflowTitle }), directives: { overflowTitle: vOverflowTitle }, template: '<span v-overflow-title="value">{{ value }}</span>' })
  const wrapper = mount(fixture, { props: { value: '完整名称' } })
  await flushPromises()
  expect(wrapper.attributes('title')).toBeUndefined()
  clipped = true; callbacks.forEach(notify => notify()); await flushPromises()
  expect(wrapper.attributes('title')).toBe('完整名称')
  await wrapper.setProps({ value: '更新后的名称' }); await flushPromises()
  expect(wrapper.attributes('title')).toBe('更新后的名称')
  clipped = false; callbacks.forEach(notify => notify()); await flushPromises()
  expect(wrapper.attributes('title')).toBeUndefined()
  wrapper.unmount()
  expect(disconnect).toHaveBeenCalledOnce()
})
