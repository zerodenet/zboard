import { defineComponent, h, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { expect, it } from 'vitest'
import DataWorkbench from './DataWorkbench.vue'

it('shows the selection area only while the parent has selected records', async () => {
  const Parent = defineComponent({
    setup() {
      const selected = ref(false)
      return () => h(DataWorkbench, { total: 1 }, {
        default: () => h('button', { onClick: () => { selected.value = !selected.value } }, selected.value ? '取消选择' : '选择记录'),
        selection: () => selected.value ? h('div', '已选择 1 条') : [],
      })
    },
  })
  const wrapper = mount(Parent)
  expect(wrapper.find('.workbench-selection').exists()).toBe(false)
  await wrapper.get('button').trigger('click')
  expect(wrapper.get('.workbench-selection').text()).toBe('已选择 1 条')
  await wrapper.get('button').trigger('click')
  expect(wrapper.find('.workbench-selection').exists()).toBe(false)
  wrapper.unmount()
})
