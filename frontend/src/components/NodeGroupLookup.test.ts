import { flushPromises, mount } from '@vue/test-utils'
import { expect, it, vi } from 'vitest'
import { fetchNodeGroupsPage } from '../api/client'
import NodeGroupLookup from './NodeGroupLookup.vue'

vi.mock('../api/client', () => ({ fetchNodeGroupsPage: vi.fn() }))
it('loads the selected group by id without an initial empty search cancelling it', async () => {
  let finish!: (page: any) => void
  vi.mocked(fetchNodeGroupsPage).mockReturnValue(new Promise(resolve => { finish = resolve }))
  const wrapper = mount(NodeGroupLookup, { props: { modelValue: 7 }, attrs: { id: 'product-group' } })
  await flushPromises()
  expect(fetchNodeGroupsPage).toHaveBeenCalledOnce()
  expect(fetchNodeGroupsPage).toHaveBeenCalledWith(expect.objectContaining({ groupId: 7, limit: 1 }), expect.anything())
  finish({ items: [{ id: 7, name: '边缘网络', code: 'edge' }], total: 1 })
  await flushPromises()
  expect(wrapper.get<HTMLInputElement>('input').element.value).toBe('边缘网络')
  expect(wrapper.get('input').attributes('id')).toBe('product-group')
  expect(fetchNodeGroupsPage).toHaveBeenCalledOnce()
  await wrapper.get('[aria-label="清除选择"]').trigger('click')
  expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([0])
  wrapper.unmount()
})
