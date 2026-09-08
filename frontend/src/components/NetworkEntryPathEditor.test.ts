import PrimeVue from 'primevue/config'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import NetworkEntryPathEditor from './NetworkEntryPathEditor.vue'
import UiSelect from './UiSelect.vue'

describe('network entry path form', () => {
  it('submits an SS node from fields and lets raw replace it without losing advanced fields', async () => {
    const wrapper = mount(NetworkEntryPathEditor, { global: { plugins: [PrimeVue] } })
    await wrapper.get('input[aria-label="代理节点地址"]').setValue('ss.example.com')
    await wrapper.get('input[aria-label="代理节点端口"]').setValue('8388')
    await wrapper.get('input[aria-label="代理密码"]').setValue('secret')
    expect(wrapper.vm.build()).toMatchObject({ outbounds: [{ protocol: { type: 'shadowsocks', port: 8388, password: 'secret' } }] })
    const mode = wrapper.findAllComponents(UiSelect)[0]!
    mode.vm.$emit('update:modelValue', 'raw'); mode.vm.$emit('change', { value: 'raw', target: { value: 'raw' } })
    await wrapper.vm.$nextTick()
    const raw = wrapper.get('textarea[aria-label="Raw 覆盖配置"]')
    expect((raw.element as HTMLTextAreaElement).value).toContain('ss.example.com')
    await raw.setValue('{"type":"vless","server":"vless.example.com","port":443,"id":"fixture-id","mux_concurrency":4,"h2":{"host":"example.com","path":"/stream"}}')
    expect(wrapper.vm.build()).toMatchObject({ outbounds: [{ protocol: { type: 'vless', mux_concurrency: 4, h2: { path: '/stream' } } }] })
    mode.vm.$emit('update:modelValue', 'form')
    await wrapper.vm.$nextTick()
    expect(wrapper.vm.build()).toMatchObject({ outbounds: [{ protocol: { type: 'shadowsocks', password: 'secret' } }] })
    wrapper.unmount()
  })
})
