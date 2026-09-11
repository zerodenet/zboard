import PrimeVue from 'primevue/config'
import {mount,flushPromises} from '@vue/test-utils'
import {describe,expect,it} from 'vitest'
import ProxyPoolEditor from './ProxyPoolEditor.vue'
import {poolFixture} from '../utils/proxyPoolGraph.fixture'
const render=()=>mount(ProxyPoolEditor,{props:{config:poolFixture()},global:{plugins:[PrimeVue]}})
const click=async(wrapper:ReturnType<typeof render>,label:string)=>{await wrapper.findAll('button').find(b=>b.text()===label)!.trigger('click');await flushPromises()}
describe('pool form and RAW synchronization',()=>{
 it('round trips selectors, relay order and advanced options through RAW and form',async()=>{
  const wrapper=render();await flushPromises()
  expect(wrapper.vm.build()).toEqual(poolFixture())
  await click(wrapper,'合并表单并查看 RAW')
  const changed=poolFixture();changed.outbounds[1]!.protocol.password='new literal secret'
  await wrapper.get('textarea[aria-label="完整代理池 RAW"]').setValue(JSON.stringify(changed))
  await click(wrapper,'覆盖并转为表单')
  expect(wrapper.vm.build()).toEqual(changed)
  expect(wrapper.get('input[aria-label="代理密码"]').element).toHaveProperty('value','new literal secret')
  await wrapper.findAll('input[aria-label="代理节点地址"]')[1]!.setValue('new.example')
  await click(wrapper,'合并表单并查看 RAW')
  expect(wrapper.vm.build().outbounds[1]!.protocol).toMatchObject({server:'new.example',password:'new literal secret'})
  wrapper.unmount()
 })
 it('keeps the previous form when RAW cannot be applied',async()=>{
  const wrapper=render();await flushPromises();await click(wrapper,'合并表单并查看 RAW')
  await wrapper.get('textarea[aria-label="完整代理池 RAW"]').setValue('{broken')
  await click(wrapper,'覆盖并转为表单')
  expect(wrapper.text()).toContain('必须是有效的 Zero JSON')
  expect(()=>wrapper.vm.build()).toThrow()
  await click(wrapper,'放弃 RAW 修改，返回表单')
  expect(wrapper.vm.build()).toEqual(poolFixture());wrapper.unmount()
 })
 it('rewrites only graph references when renaming a node',async()=>{
  const wrapper=render();await flushPromises()
  await wrapper.findAll('input[aria-label="代理标识"]')[0]!.setValue('renamed');await flushPromises()
  const graph=wrapper.vm.build()
  expect(graph.outbound_groups[0]!.outbounds).toEqual(['renamed','p2'])
  expect(graph.outbound_groups[1]!.proxies).toEqual(['p2','renamed'])
  expect(graph.outbounds[0]!.tag).toBe('renamed');wrapper.unmount()
 })
 it('merges newly entered nodes into a new pool',async()=>{
  const wrapper=mount(ProxyPoolEditor,{global:{plugins:[PrimeVue]}});await flushPromises()
  await wrapper.get('input[aria-label="代理节点地址"]').setValue('one.example')
  await wrapper.get('input[aria-label="代理密码"]').setValue('one')
  await click(wrapper,'添加代理节点')
  await wrapper.findAll('input[aria-label="代理节点地址"]')[1]!.setValue('two.example')
  await wrapper.findAll('input[aria-label="代理密码"]')[1]!.setValue('two')
  expect(wrapper.vm.build().outbound_groups[0]!.outbounds).toEqual(['proxy-1','proxy-2'])
  wrapper.unmount()
 })
})
