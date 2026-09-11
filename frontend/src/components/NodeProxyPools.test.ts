import PrimeVue from 'primevue/config'
import {mount,flushPromises} from '@vue/test-utils'
import {beforeEach,describe,expect,it,vi} from 'vitest'
import {poolFixture} from '../utils/proxyPoolGraph.fixture'
import NodeProxyPools from './NodeProxyPools.vue'
const mocks=vi.hoisted(()=>({list:vi.fn(),save:vi.fn(),sync:vi.fn(),remove:vi.fn(),detail:vi.fn(),runtime:vi.fn()}))
vi.mock('../api/client',()=>({fetchNodeProxyPools:mocks.list,saveNodeProxyPool:mocks.save,syncNodeProxyPool:mocks.sync,deleteNodeProxyPool:mocks.remove,fetchNodeProxyPoolConfig:mocks.detail,fetchNodeProxyPoolRuntime:mocks.runtime}))
vi.mock('../utils/feedback',()=>({confirmAction:vi.fn(async()=>true),notify:vi.fn()}))
function render(){return mount(NodeProxyPools,{props:{nodeId:7},global:{plugins:[PrimeVue],stubs:{teleport:true}}})}
describe('node shared pools',()=>{
 beforeEach(()=>{vi.clearAllMocks();mocks.list.mockResolvedValue([{id:3,node_id:7,name:'现有池',revision:4,entry_count:2}]);mocks.save.mockResolvedValue({});mocks.sync.mockResolvedValue({});mocks.detail.mockResolvedValue({pool:{id:3,node_id:7,name:'现有池',revision:4},config:poolFixture(),compiled:{}})})
 it('keeps encrypted members when renaming a node pool',async()=>{
  const graph=poolFixture();mocks.detail.mockResolvedValueOnce({pool:{id:3,node_id:7,name:'现有池',revision:4},config:{target:graph.target,outbound_groups:graph.outbound_groups,outbounds:graph.outbounds}})
  const wrapper=render();await flushPromises();expect(mocks.list).toHaveBeenCalledWith(7)
  await wrapper.findAll('button').find(b=>b.text()==='编辑')!.trigger('click');await flushPromises()
  expect(mocks.detail).toHaveBeenCalledWith(3);expect(wrapper.get('input[aria-label="代理节点地址"]').element).toHaveProperty('value','proxy.example')
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
 it('edits existing credentials and leaves the draft intact when node read fails',async()=>{
  mocks.runtime.mockRejectedValue(new Error('SSH 不可用'))
  const wrapper=render();await flushPromises()
  await wrapper.findAll('button').find(b=>b.text()==='编辑')!.trigger('click');await flushPromises()
  await wrapper.get('input[aria-label="代理密码"]').setValue('changed secret')
  await wrapper.findAll('button').find(b=>b.text()==='读取节点配置')!.trigger('click');await flushPromises()
  expect(wrapper.text()).toContain('SSH 不可用')
  await wrapper.findAll('button').find(b=>b.text()==='保存并发布到本节点')!.trigger('click');await flushPromises()
  expect(mocks.save.mock.calls[0]![1].config.outbounds[1].protocol.password).toBe('changed secret')
  wrapper.unmount()
 })
 it('discards delayed secret reads after switching nodes',async()=>{
  let resolve!:(value:any)=>void
  mocks.detail.mockReturnValueOnce(new Promise(r=>{resolve=r}))
  const wrapper=render();await flushPromises()
  await wrapper.findAll('button').find(b=>b.text()==='编辑')!.trigger('click')
  await wrapper.setProps({nodeId:8});await flushPromises()
  resolve({pool:{id:3,node_id:7,name:'old',revision:4},config:poolFixture()});await flushPromises()
  expect(wrapper.find('input[aria-label="代理密码"]').exists()).toBe(false)
  expect(mocks.save).not.toHaveBeenCalled();wrapper.unmount()
 })
 it('creates a subscription-maintained pool without replacing the retained RAW editor contract',async()=>{
  const wrapper=render();await flushPromises()
  await wrapper.findAll('button').find(b=>b.text()==='创建代理池')!.trigger('click');await flushPromises()
  await wrapper.findAll('button').find(b=>b.text()==='订阅维护')!.trigger('click');await flushPromises()
  await wrapper.find('input').setValue('订阅池')
  await wrapper.get('input[aria-label="订阅地址"]').setValue('https://example.com/sub/token')
  expect(wrapper.get('aside[aria-label="订阅格式帮助"] a').attributes('href')).toBe('https://docs.zerodenet.org/projects/zboard/guides/network-fronting')
  expect(wrapper.find('details.pool-subscription-support').exists()).toBe(false)
  await wrapper.findAll('button').find(b=>b.text()==='保存订阅设置')!.trigger('click');await flushPromises()
  expect(mocks.save).toHaveBeenCalledWith(0,expect.objectContaining({node_id:7,name:'订阅池',subscription_url:'https://example.com/sub/token',subscription_format:'auto',subscription_user_agent:'Clash.Meta',auto_sync:false,sync_interval_seconds:86400}))
  expect(mocks.save.mock.calls[0]![1]).not.toHaveProperty('config')
  wrapper.unmount()
 })

})
