import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import ProxyPoolSelection from './ProxyPoolSelection.vue'
import UiCheckbox from './UiCheckbox.vue'
const list = vi.hoisted(() => vi.fn())
vi.mock('../api/client', () => ({ fetchNodeProxyPools: list }))
vi.mock('./NodeProxyPools.vue', async () => { const { defineComponent } = await import('vue'); return { default: defineComponent({ name: 'PoolEditorStub', emits: ['saved'], setup(_, { expose }) { expose({ openCreate: vi.fn() }); return {} }, template: '<div class="pool-editor-stub" />' }) } })
beforeEach(() => { list.mockReset(); list.mockResolvedValue([{ id: 3, name: 'shared', nodes: [{ id: 7, name: 'VPS A' }] }]) })
it('selects an existing pool without copying its configuration', async () => {
 const wrapper = mount(ProxyPoolSelection, { props: { modelValue: [] } }); await flushPromises()
 expect(list).toHaveBeenCalledWith(); expect(wrapper.text()).toContain('VPS A')
 wrapper.findComponent(UiCheckbox).vm.$emit('update:modelValue', true)
 expect(wrapper.emitted('update:modelValue')).toEqual([[[3]]]); wrapper.unmount()
})
it('selects a newly created independent pool for the pending server', async () => {
 const wrapper = mount(ProxyPoolSelection, { props: { modelValue: [3] } }); await flushPromises()
 wrapper.findComponent({ name: 'PoolEditorStub' }).vm.$emit('saved', { id: 8, name: 'new shared pool' }); await flushPromises()
 expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[3, 8]])
 wrapper.unmount()
})
