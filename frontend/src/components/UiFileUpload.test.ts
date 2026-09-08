import PrimeVue from 'primevue/config'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import UiFileUpload from './UiFileUpload.vue'

describe('offline file selection', () => {
 it('resets the picker before a busy-state rerender and allows selecting the same package again', async () => {
  const wrapper = mount(UiFileUpload, { attrs: { accept: '.zbplugin', chooseLabel: '离线导入' }, global: { plugins: [PrimeVue] } })
  const file = new File(['signed fixture'], 'welcome.zbplugin', { type: 'application/octet-stream' })
  const first = wrapper.get('input[type="file"]')
  Object.defineProperty(first.element, 'files', { value: [file], configurable: true })
  await first.trigger('change'); await flushPromises()
  expect(wrapper.emitted('select')?.[0]).toEqual([[file]])
  expect(wrapper.get('input[type="file"]').element).not.toBe(first.element)
  await wrapper.setProps({ disabled: true }); await wrapper.setProps({ disabled: false })
  const second = wrapper.get('input[type="file"]')
  Object.defineProperty(second.element, 'files', { value: [file], configurable: true })
  await second.trigger('change'); await flushPromises()
  expect(wrapper.emitted('select')).toHaveLength(2)
  wrapper.unmount()
 })
})
