import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, expect, it } from 'vitest'
import Tooltip from './Tooltip.vue'
let wrapper: VueWrapper | undefined
afterEach(() => { wrapper?.unmount(); document.body.innerHTML = '' })
it('keeps the default tooltip uncontrolled and available on keyboard focus', async () => {
  wrapper = mount(Tooltip, { attachTo: document.body, props: { text: '完整内容', delayDuration: 0 }, slots: { default: '<button>查看</button>' } })
  wrapper.get('button').element.focus(); await flushPromises()
  expect(document.querySelector('.ui-tooltip-content')?.textContent).toContain('完整内容')
  await wrapper.get('button').trigger('keydown', { key: 'Escape' }); await flushPromises()
  expect(document.querySelector('.ui-tooltip-content')).toBeNull()
})
it('lets a controlled chart tooltip request dismissal', async () => {
  wrapper = mount(Tooltip, { attachTo: document.body, props: { text: '金额', open: true }, slots: { default: '<button>数据点</button>' } })
  await flushPromises()
  await wrapper.get('button').trigger('keydown', { key: 'Escape' }); await flushPromises()
  expect(wrapper.emitted('update:open')?.at(-1)).toEqual([false])
  await wrapper.setProps({ open: false }); await flushPromises()
  expect(document.querySelector('.ui-tooltip-content')).toBeNull()
})
