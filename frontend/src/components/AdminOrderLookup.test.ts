import { shallowMount, flushPromises } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import AdminOrderLookup from './AdminOrderLookup.vue'
import UiAutocomplete from './UiAutocomplete.vue'
describe('assignment lookup', () => {
  it('ignores stale search results and clears selected identity while typing', async () => {
    let first!: (value: unknown) => void
    const load = vi.fn().mockImplementationOnce(() => new Promise(resolve => { first = resolve })).mockResolvedValueOnce({ items: [{ id: 2, label: 'new' }], total: 1 })
    const wrapper = shallowMount(AdminOrderLookup, { props: { modelValue: { id: 7, label: 'buyer' }, label: '用户', fetchPage: load } })
    const input = wrapper.getComponent(UiAutocomplete)
    input.vm.$emit('update:modelValue', 'new'); expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([null])
    input.vm.$emit('complete', { query: 'old' }); input.vm.$emit('complete', { query: 'new' }); await flushPromises()
    expect(load.mock.calls[0][2].aborted).toBe(true)
    first({ items: [{ id: 1, label: 'old' }], total: 1 }); await flushPromises()
    expect(input.attributes('suggestions')).toBe('[object Object]')
    expect((input.vm.$attrs.suggestions as any[])[0].id).toBe(2)
    wrapper.unmount()
  })
})
