import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import PrimeVue from 'primevue/config'
import TableText from './TableText.vue'
import EntityReference from './EntityReference.vue'

describe('List text roles', () => {
  it('exposes full auxiliary text on click and retains zero as a value', async () => {
    const value = '关联套餐名称很长–东京线路与海外备用线路'
    const wrapper = mount(TableText, { props: { value }, attachTo: document.body, global: { plugins: [PrimeVue] } })
    expect(wrapper.get('button').attributes('title')).toBe(value)
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(document.querySelector('.table-text-detail')?.textContent).toBe(value)
    await wrapper.setProps({ value: 0 })
    expect(wrapper.get('button').text()).toBe('0')
    expect(document.querySelector('.table-text-detail')?.textContent).toBe('0')
    wrapper.unmount()
  })
  it('renders a primary entity name directly and uses expandable text only for related entities', async () => {
    const reference = { id: 1, kind: 'node', display_name: '日本东京–完整节点名称', secondary: '', missing: false }
    const wrapper = mount(EntityReference, { props: { reference, showId: false }, global: { plugins: [PrimeVue] } })
    expect(wrapper.get('strong').text()).toBe(reference.display_name)
    expect(wrapper.find('button').exists()).toBe(false)
    await wrapper.setProps({ compact: true })
    expect(wrapper.get('button').attributes('title')).toBe(reference.display_name)
    wrapper.unmount()
  })
})
