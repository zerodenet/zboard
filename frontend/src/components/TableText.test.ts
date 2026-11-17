import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import TableText from './TableText.vue'
import EntityReference from './EntityReference.vue'

let clipped = false
const notify: Array<() => void> = []
beforeEach(() => {
  clipped = false; notify.length = 0
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(100)
  vi.spyOn(HTMLElement.prototype, 'scrollWidth', 'get').mockImplementation(function (this: HTMLElement) { return (this as HTMLElement).classList.contains('table-secondary-text') && clipped ? 300 : 100 })
  vi.stubGlobal('ResizeObserver', class { constructor(callback: (...args: any[]) => void) { notify.push(() => callback([])) } observe() {} disconnect() {} })
})
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); document.body.innerHTML = '' })
async function resize(value: boolean) { clipped = value; notify.forEach(callback => callback()); await flushPromises() }

describe('List text roles', () => {
  it('does not offer a popup for missing content', async () => {
    const wrapper = mount(TableText)
    for (const value of [null, undefined, '', '   ']) {
      await wrapper.setProps({ value })
      expect(wrapper.text()).toBe('-')
      expect(wrapper.find('button').exists()).toBe(false)
      expect(wrapper.find('[aria-label="无数据"]').exists()).toBe(true)
    }
    wrapper.unmount()
  })
  it('exposes full auxiliary text on click and retains zero as a value', async () => {
    clipped = true
    const value = '关联套餐名称很长–东京线路与海外备用线路'
    const wrapper = mount(TableText, { props: { value }, attachTo: document.body, global: { plugins: [] } })
    await flushPromises()
    expect(wrapper.get('button').attributes('title')).toBeUndefined()
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(document.querySelector('.table-text-detail')?.textContent).toBe(value)
    const scope = wrapper.get('button').element.getAttributeNames().find(name => name.startsWith('data-v-'))!
    expect(scope).toBeTruthy()
    expect(document.querySelector('.table-text-detail')?.hasAttribute(scope)).toBe(true)
    await resize(false)
    await wrapper.setProps({ value: 0 })
    expect(wrapper.text()).toBe('0')
    expect(wrapper.find('button').exists()).toBe(false)
    expect(document.querySelector('.table-text-detail')).toBeNull()
    wrapper.unmount()
  })
  it('renders a primary entity name directly and uses expandable text only for related entities', async () => {
    const reference = { id: 1, kind: 'node', display_name: '日本东京–完整节点名称', secondary: '', missing: false }
    const wrapper = mount(EntityReference, { props: { reference, showId: false }, global: { plugins: [] } })
    expect(wrapper.get('strong').text()).toBe(reference.display_name)
    expect(wrapper.find('button').exists()).toBe(false)
    await wrapper.setProps({ compact: true })
    await flushPromises()
    expect(wrapper.find('button').exists()).toBe(false)
    expect(wrapper.text()).toBe(reference.display_name)
    await resize(true)
    expect(wrapper.get('button').attributes('aria-label')).toContain(reference.display_name)
    wrapper.unmount()
  })
})
