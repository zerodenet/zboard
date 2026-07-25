import { describe, expect, it } from 'vitest'
import { ref } from 'vue'
import { useSelectionScope } from './useSelectionScope'

describe('useSelectionScope', () => {
  it('distinguishes explicit page selection from the complete filtered result', () => {
    const items = ref([{ id: 1 }, { id: 2 }])
    const total = ref(1000)
    const selection = useSelectionScope({ items, total, key: item => item.id })

    selection.togglePage(true)
    expect(selection.selectedIDs.value).toEqual([1, 2])
    expect(selection.allPageSelected.value).toBe(true)
    expect(selection.canSelectAllMatching.value).toBe(true)
    expect(selection.selectedCount.value).toBe(2)

    selection.selectAllMatching()
    expect(selection.allMatching.value).toBe(true)
    expect(selection.selectedIDs.value).toEqual([])
    expect(selection.selectedCount.value).toBe(1000)
    expect(selection.isSelected(999)).toBe(true)

    selection.clear()
    expect(selection.selectedCount.value).toBe(0)
  })

  it('reports an indeterminate current page without losing cross-page ids', () => {
    const items = ref([{ id: 3 }, { id: 4 }])
    const total = ref(10)
    const selection = useSelectionScope({ items, total, key: item => item.id })

    selection.toggle(1, true)
    selection.toggle(3, true)
    expect(selection.pageSelectionIndeterminate.value).toBe(true)
    selection.togglePage(false)
    expect(selection.selectedIDs.value).toEqual([1])
  })
})
