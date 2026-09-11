import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import NetworkEntryPathEditor from './NetworkEntryPathEditor.vue'

describe('network entry path form', () => {
 it('edits an existing SS node while preserving credentials and opaque options', async () => {
  const outbound={tag:'ss',protocol:{type:'shadowsocks',server:'old.example',port:443,cipher:'aes-128-gcm',password:' secret ',advanced:{mode:'keep'}}}
  const wrapper=mount(NetworkEntryPathEditor,{props:{outbound},global:{plugins:[PrimeVue]}})
  expect(wrapper.vm.build()).toEqual(outbound)
  await wrapper.get('input[aria-label="代理节点地址"]').setValue('new.example')
  await wrapper.get('input[aria-label="代理节点端口"]').setValue('8388')
  expect(wrapper.vm.build()).toEqual({...outbound,protocol:{...outbound.protocol,server:'new.example',port:8388}})
  expect(wrapper.emitted('summary')?.at(-1)?.[0]).toBe('shadowsocks · new.example')
  wrapper.unmount()
 })
 it('preserves protocol RAW for nodes without a dedicated form',async()=>{
  const outbound={tag:'direct',protocol:{type:'direct',bind_interface:'eth0'}}
  const wrapper=mount(NetworkEntryPathEditor,{props:{outbound},global:{plugins:[PrimeVue]}})
  expect(wrapper.vm.build()).toEqual(outbound)
  await wrapper.get('textarea').setValue(JSON.stringify({...outbound,protocol:{type:'direct',bind_interface:'eth1'}}))
  expect(wrapper.vm.build().protocol.bind_interface).toBe('eth1')
  wrapper.unmount()
 })
})
