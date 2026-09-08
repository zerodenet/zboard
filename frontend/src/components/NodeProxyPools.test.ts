import PrimeVue from 'primevue/config'
import {mount,flushPromises} from '@vue/test-utils'
import {beforeEach,describe,expect,it,vi} from 'vitest'
import NodeProxyPools from './NodeProxyPools.vue'
const mocks=vi.hoisted(()=>({list:vi.fn(),save:vi.fn(),remove:vi.fn()}))
vi.mock('../api/client',()=>({fetchNodeProxyPools:mocks.list,saveNodeProxyPool:mocks.save,deleteNodeProxyPool:mocks.remove}))
vi.mock('../utils/feedback',()=>({confirmAction:vi.fn(async()=>true),notify:vi.fn()}))
function render(){return mount(NodeProxyPools,{props:{nodeId:7},global:{plugins:[PrimeVue],stubs:{teleport:true}}})}
describe('node shared pools',()=>{
 beforeEach(()=>{vi.clearAllMocks();mocks.list.mockResolvedValue([{id:3,node_id:7,name:'现有池',revision:4,entry_count:2}]);mocks.save.mockResolvedValue({})})
 it('keeps encrypted members when renaming a node pool',async()=>{
  const wrapper=render();await flushPromises();expect(mocks.list).toHaveBeenCalledWith(7)
  await wrapper.findAll('button').find(b=>b.text()==='编辑')!.trigger('click');await flushPromises()
  expect(wrapper.text()).toContain('保留现有代理和凭据')
  await wrapper.get('input').setValue('新名称')
  await wrapper.findAll('button').find(b=>b.text()==='保存并发布到本节点')!.trigger('click');await flushPromises()
  expect(mocks.save).toHaveBeenCalledWith(3,{node_id:7,name:'新名称',revision:4})
  wrapper.unmount()
 })
 it('shows a referenced-pool deletion error instead of removing the row',async()=>{
  mocks.remove.mockRejectedValue({response:{data:{message:'仍有 2 条前置服务引用该池'}}})
  const wrapper=render();await flushPromises()
  await wrapper.findAll('button').find(b=>b.text()==='删除')!.trigger('click');await flushPromises()
  expect(wrapper.text()).toContain('仍有 2 条前置服务引用该池');expect(wrapper.text()).toContain('现有池');wrapper.unmount()
 })
})
