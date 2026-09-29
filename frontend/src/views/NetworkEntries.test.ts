import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import NetworkEntries from './NetworkEntries.vue'
const mocks = vi.hoisted(() => ({list: vi.fn(), save: vi.fn(), remove: vi.fn(), endpoints: vi.fn(), confirm: vi.fn(), pools: vi.fn()}))
vi.mock('../api/client', () => ({fetchNetworkEntries: mocks.list, saveNetworkEntry: mocks.save, deleteNetworkEntry: mocks.remove, fetchProtocolEndpoints: mocks.endpoints, fetchNodeProxyPools: mocks.pools, fetchNodeGroupsPage: vi.fn(async()=>({items:[],total:0}))}))
vi.mock('../utils/feedback', () => ({confirmAction: mocks.confirm, notify: vi.fn()}))
const entry = {id: 7, name: '香港入口', node_id: 1, node_name: '香港 A', landing_node_id: 2, endpoint_id: 3, endpoint_name: '日本 B SS', address: 'entry.example.com', port: 10000, public_port: 20000, enabled: true, has_path: true, revision: 4, pending: false, last_error: ''}
function render() { return mount(NetworkEntries, {global: {plugins: [], stubs: {teleport: true, ModalDialog: {props: ['open', 'title'], template: '<div v-if="open"><h2>{{ title }}</h2><slot/><slot name="footer"/></div>'}, NodeLookup: {props: ['modelValue'], emits: ['update:modelValue'], template: '<input class="node-lookup-test" type="number" :value="modelValue" @input="$emit(\'update:modelValue\', Number($event.target.value))" />'}, RowActions: {template: '<div><slot /></div>'}}}}) }
async function click(wrapper: ReturnType<typeof render>, text: string) { await wrapper.findAll('button').find(button => button.text() === text)!.trigger('click'); await flushPromises() }
describe('Network entry management', () => {
 beforeEach(() => {vi.clearAllMocks();mocks.pools.mockResolvedValue([{id: 8, name: "A 的共享池"}]);mocks.list.mockResolvedValue([entry]);mocks.save.mockResolvedValue(entry);mocks.endpoints.mockResolvedValue([{id: 3, name: '日本 B SS', is_active: true}]);mocks.confirm.mockResolvedValue(true)})
 it('loads B selection when editing and preserves encrypted path without resending it', async () => {
  const wrapper = render(); await flushPromises(); await click(wrapper, '编辑')
  expect(mocks.endpoints).toHaveBeenCalledWith(2)
  expect(wrapper.text()).toContain('将入口加入套餐的节点组后，系统会生成父协议凭据')
  const input = wrapper.findAll('input').find(input => input.element.value === entry.name)!
  await input.setValue('新入口'); await click(wrapper, '保存并发布')
  expect(mocks.save).toHaveBeenCalledWith(7, expect.objectContaining({name: '新入口', endpoint_id: 3, node_id: 1, revision: 4}))
  expect(mocks.save.mock.calls[0][1]).not.toHaveProperty('path_config')
  wrapper.unmount()
 })
 it('registers an external forward without requiring or deploying an A node', async () => {
  const wrapper=render(); await flushPromises(); await click(wrapper,'创建入口')
  const landing=wrapper.find('input.node-lookup-test');await landing.setValue('2');await flushPromises()
  const parent=wrapper.findAllComponents({name:'UiSelect'}).find(select=>select.props('options')?.some((option:any)=>option.value===3))!
  parent.vm.$emit('update:modelValue',3)
  await wrapper.find('input[placeholder="例如：香港入口"]').setValue('External entry')
  await wrapper.find('input[placeholder="entry.example.com"]').setValue('external.example.test')
  await click(wrapper,'保存入口')
  expect(mocks.save).toHaveBeenCalledWith(0,expect.objectContaining({deployment_mode:'external',node_id:0,parent_protocol_id:3,address:'external.example.test',port:10000,public_port:10000,proxy_pool_id:0,path_config:{}}))
  expect(mocks.pools).not.toHaveBeenCalled();wrapper.unmount()
 })
 it('saves a shared pool as an optional A route while keeping B as parent', async () => {
  mocks.list.mockResolvedValue([{...entry, proxy_pool_id: 8, has_path: false}])
  const wrapper = render(); await flushPromises(); await click(wrapper, '编辑')
  expect(mocks.pools).toHaveBeenCalledWith(1)
  expect(wrapper.text()).toContain('A 的共享池')
  await click(wrapper, '保存并发布')
  expect(mocks.save).toHaveBeenCalledWith(7, expect.objectContaining({node_id: 1, parent_protocol_id: 3, proxy_pool_id: 8, path_config: {}, node_group_membership_changes: []}))
  wrapper.unmount()
 })
 it('explicitly clears proxy routing for a direct entry', async () => {
  mocks.list.mockResolvedValue([{...entry, has_path: false}])
  const wrapper = render(); await flushPromises(); await click(wrapper, '编辑'); await click(wrapper, '保存并发布')
  expect(mocks.save).toHaveBeenCalledWith(7, expect.objectContaining({parent_protocol_id: 3, proxy_pool_id: 0, path_config: {}}))
  wrapper.unmount()
 })
 it('keeps the editor and field errors visible when binding fails', async () => {
  mocks.save.mockRejectedValue({response: {data: {message: '保存失败', error: {fields: {configuration: 'A 与 B 必须是不同节点'}}}}})
  const wrapper = render(); await flushPromises(); await click(wrapper, '编辑'); await click(wrapper, '保存并发布')
  expect(wrapper.text()).toContain('A 与 B 必须是不同节点'); expect(wrapper.text()).toContain('编辑前置转发服务'); wrapper.unmount()
 })
 it('removes only the entry and describes preservation of B direct access', async () => {
  const wrapper = render(); await flushPromises(); mocks.list.mockResolvedValue([]); await click(wrapper, '删除')
  expect(mocks.remove).toHaveBeenCalledWith(7); expect(mocks.confirm).toHaveBeenCalledWith(expect.objectContaining({message: expect.stringContaining('单独授权的直连线路保留')})); wrapper.unmount()
 })
})
