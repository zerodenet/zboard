import { defineComponent, nextTick } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import WorkbenchFilterLookup from './WorkbenchFilterLookup.vue'

const Lookup = defineComponent({
  props: ['modelValue'],
  emits: ['update:modelValue', 'select'],
  template: '<button type="button" @click="$emit(\'update:modelValue\', 7); $emit(\'select\', { id: 7, name: \'Fixture VPS\' })">选择服务器</button>',
})

describe('WorkbenchFilterLookup', () => {
  it('uses the shared filter chip, applies one selected ID and clears it', async () => {
    const wrapper = mount(WorkbenchFilterLookup, {
      attachTo: document.body,
      props: { kind: 'node', label: '服务器', placeholder: '搜索服务器', modelValue: 0 },
      global: { stubs: { NodeLookup: Lookup, NodeGroupLookup: Lookup } },
    })
    await wrapper.get('.workbench-filter-chip-trigger').trigger('click')
    await nextTick()
    const choice = document.body.querySelector<HTMLButtonElement>('.workbench-filter-popover button')!
    choice.click()
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([7])
    expect(wrapper.emitted('apply')).toHaveLength(1)
    expect(wrapper.get('.workbench-filter-chip-trigger').text()).toContain('Fixture VPS')
    await wrapper.get('.workbench-filter-chip-clear').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.[1]).toEqual([0])
    expect(wrapper.emitted('apply')).toHaveLength(2)
    wrapper.unmount()
  })
})
