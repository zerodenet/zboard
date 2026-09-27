import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import PluginSlot from './PluginSlot.vue'

const fetchSlots = vi.hoisted(() => vi.fn())
vi.mock('../api/plugins', () => ({ fetchPluginSlots: fetchSlots }))

describe('optional plugin slot', () => {
  it('stays out of the form layout until its frame is ready', async () => {
    fetchSlots.mockResolvedValueOnce([{ plugin_id: 'zboard.oauth', generation: 1, slot: { id: 'login', surface: 'public', slot: 'auth.login.methods', title: 'OAuth', entrypoint: 'ui/login.html' } }])
    const wrapper = mount(PluginSlot, {
      props: { name: 'auth.login.methods', surface: 'public', optional: true },
      global: { stubs: { PluginFrame: { name: 'PluginFrame', template: '<div class="test-plugin-frame" />', emits: ['ready'] } } },
    })
    await flushPromises()
    expect(wrapper.get('.plugin-slot').classes()).toContain('plugin-slot--pending')
    wrapper.get('.test-plugin-frame')
    wrapper.findComponent({ name: 'PluginFrame' }).vm.$emit('ready')
    await flushPromises()
    expect(wrapper.get('.plugin-slot').classes()).not.toContain('plugin-slot--pending')
    wrapper.unmount()
  })
})
