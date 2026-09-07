import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import PrimeVue from 'primevue/config'
import DataTable from './DataTable.vue'
const originalWidth = window.innerWidth
afterEach(() => Object.defineProperty(window, 'innerWidth', { configurable: true, value: originalWidth }))

describe('DataTable', () => {
  it('exposes caption, row count, density and bounded table width semantics', () => {
    const wrapper = mount(DataTable, {
      props: { caption: '节点资产列表', rowCount: 1000, density: 'comfortable', selectable: true, minWidth: 960 },
      slots: { default: '<tbody><tr><td>节点一</td></tr></tbody>' },
    })

    const table = wrapper.get('table')
    expect(table.attributes('aria-rowcount')).toBe('1000')
    expect(table.attributes('data-density')).toBe('comfortable')
    expect(table.attributes('data-selectable')).toBe('true')
    expect(table.attributes('style')).toContain('min-width: 960px')
    expect(wrapper.get('caption').text()).toBe('节点资产列表')
    wrapper.unmount()
  })

  it('can reveal auxiliary columns without losing primary names and updates group spans on resize', async () => {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 390 })
    const name = '需要完整展示的节点名称–东京优化专线'
    const wrapper = mount(DataTable, {
      props: { caption: '节点', minWidth: 900 }, global: { plugins: [PrimeVue] },
      slots: { default: `<thead><tr><th class="table-primary-column" data-column-priority="1">节点</th><th data-column-priority="2">关联套餐</th><th data-column-priority="3">统计</th><th>状态</th></tr></thead><tbody><tr><td>${name}</td><td>关联套餐完整名称</td><td>0</td><td>运行中</td></tr></tbody>` },
    })
    await flushPromises()
    expect(wrapper.emitted('visibleColumnCount')?.at(-1)).toEqual([2])
    expect(wrapper.get('table').attributes('style')).toContain('min-width: 100%')
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('visibleColumnCount')?.at(-1)).toEqual([4])
    expect(wrapper.get('table').attributes('style')).toContain('min-width: 900px')
    expect(wrapper.get('table').text()).toContain(name)
    expect(wrapper.get('table').text()).toContain('关联套餐完整名称')
    await wrapper.get('button').trigger('click')
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 900 })
    window.dispatchEvent(new Event('resize'))
    await flushPromises()
    expect(wrapper.emitted('visibleColumnCount')?.at(-1)).toEqual([3])
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 1440 })
    window.dispatchEvent(new Event('resize'))
    await flushPromises()
    expect(wrapper.find('button').exists()).toBe(false)
    expect(wrapper.emitted('visibleColumnCount')?.at(-1)).toEqual([4])
    wrapper.unmount()
  })
})
