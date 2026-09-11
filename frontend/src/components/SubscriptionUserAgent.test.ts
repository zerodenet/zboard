import { defineComponent } from 'vue'
import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import SubscriptionUserAgent from './SubscriptionUserAgent.vue'
import UiSelect from './UiSelect.vue'
function render(value: string) {
 return mount(defineComponent({ components: { SubscriptionUserAgent }, data: () => ({ value }), template: '<SubscriptionUserAgent v-model="value" />' }), { global: { plugins: [PrimeVue] } })
}
describe('subscription client selection', () => {
 it('uses presets and allows explicit custom input', async () => {
  const wrapper = render('Clash.Meta')
  expect(wrapper.find('input[aria-label="订阅 User-Agent"]').exists()).toBe(false)
  const select = wrapper.getComponent(UiSelect)
  for (const value of ['sing-box', 'v2rayN']) {
   expect(select.props('options')).toEqual(expect.arrayContaining([expect.objectContaining({ value })]))
   select.vm.$emit('update:modelValue', value)
   await wrapper.vm.$nextTick()
   expect(wrapper.vm.value).toBe(value)
  }
  select.vm.$emit('update:modelValue', 'ZNet-Sink/0.0.1')
  await wrapper.vm.$nextTick()
  expect(wrapper.vm.value).toBe('ZNet-Sink/0.0.1')
  select.vm.$emit('update:modelValue', 'custom')
  await wrapper.vm.$nextTick()
  await wrapper.get('input[aria-label="订阅 User-Agent"]').setValue('custom-client/2')
  expect(wrapper.vm.value).toBe('custom-client/2')
  expect(wrapper.text()).toContain('返回内容需使用受支持的订阅格式')
  select.vm.$emit('update:modelValue', 'Clash.Meta')
  await wrapper.vm.$nextTick()
  expect(wrapper.vm.value).toBe('Clash.Meta')
  expect(wrapper.find('input[aria-label="订阅 User-Agent"]').exists()).toBe(false)
  wrapper.unmount()
 })
 it('preserves existing custom identifiers', () => {
  const wrapper = render('existing-client/1')
  expect(wrapper.getComponent(UiSelect).props('modelValue')).toBe('custom')
  expect((wrapper.get('input[aria-label="订阅 User-Agent"]').element as HTMLInputElement).value).toBe('existing-client/1')
  expect(wrapper.vm.value).toBe('existing-client/1')
  wrapper.unmount()
 })
})
