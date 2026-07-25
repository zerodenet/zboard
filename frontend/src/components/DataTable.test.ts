import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import DataTable from './DataTable.vue'

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
  })
})
