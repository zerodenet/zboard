import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import SortableHeader from './SortableHeader.vue'

describe('SortableHeader', () => {
  it('announces current direction and requests the inverse direction on activation', async () => {
    const wrapper = mount({
      components: { SortableHeader },
      template: '<table><thead><tr><SortableHeader field="name" label="节点" sort-field="name" direction="asc" @sort="sorted = $event" /></tr></thead></table>',
      data: () => ({ sorted: '' }),
    })

    expect(wrapper.get('th').attributes('aria-sort')).toBe('ascending')
    expect(wrapper.get('button').attributes('aria-label')).toContain('降序')
    await wrapper.get('button').trigger('click')
    expect((wrapper.vm as any).sorted).toBe('name')
  })
})
