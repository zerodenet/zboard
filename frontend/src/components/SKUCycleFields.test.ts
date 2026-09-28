import { mount } from '@vue/test-utils'
import { expect, it } from 'vitest'
import SKUCycleFields from './SKUCycleFields.vue'
import UiSelect from './UiSelect.vue'
it('preserves unusual existing periods and exposes custom units and counts', async () => {
  const view = mount(SKUCycleFields, { props: { idPrefix: 'test', billingUnit: 'day', billingValue: 45 } })
  expect(view.text()).toContain('自定义周期');expect(view.text()).toContain('周期数量')
  view.findAllComponents(UiSelect)[0].vm.$emit('update:modelValue','year')
  expect(view.emitted('update:billingUnit')![0]).toEqual(['year']);expect(view.emitted('update:billingValue')![0]).toEqual([1])
  await view.setProps({billingUnit:'year',billingValue:1})
  expect(view.text()).not.toContain('周期数量')
  view.findComponent(UiSelect).vm.$emit('update:modelValue','custom');await view.vm.$nextTick()
  expect(view.text()).toContain('周期数量');expect(view.emitted('update:billingUnit')).toHaveLength(1)
  view.unmount()
})
