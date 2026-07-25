import { computed, shallowRef, type Ref } from 'vue'

export function useSelectionScope<T, K extends string | number>(options: {
  items: Ref<readonly T[]>
  total: Ref<number>
  key: (item: T) => K
}) {
  const selectedIDs = shallowRef<K[]>([])
  const allMatching = shallowRef(false)
  const selectedIDSet = computed(() => new Set(selectedIDs.value))
  const selectedOnPage = computed(() => options.items.value.reduce((count, item) => count + (selectedIDSet.value.has(options.key(item)) ? 1 : 0), 0))
  const selectedCount = computed(() => allMatching.value ? options.total.value : selectedIDs.value.length)
  const allPageSelected = computed(() => Boolean(options.items.value.length) && (allMatching.value || selectedOnPage.value === options.items.value.length))
  const pageSelectionIndeterminate = computed(() => !allMatching.value && selectedOnPage.value > 0 && selectedOnPage.value < options.items.value.length)
  const canSelectAllMatching = computed(() => !allMatching.value && options.total.value > selectedIDs.value.length && allPageSelected.value)

  function isSelected(id: K) {
    return allMatching.value || selectedIDSet.value.has(id)
  }

  function toggle(id: K, selected: boolean) {
    if (allMatching.value) return
    const next = new Set(selectedIDs.value)
    if (selected) next.add(id)
    else next.delete(id)
    selectedIDs.value = [...next]
  }

  function togglePage(selected: boolean) {
    if (allMatching.value) return
    const next = new Set(selectedIDs.value)
    for (const item of options.items.value) {
      const id = options.key(item)
      if (selected) next.add(id)
      else next.delete(id)
    }
    selectedIDs.value = [...next]
  }

  function selectAllMatching() {
    if (!options.total.value) return
    allMatching.value = true
    selectedIDs.value = []
  }

  function clear() {
    allMatching.value = false
    selectedIDs.value = []
  }

  return {
    selectedIDs,
    allMatching,
    selectedCount,
    allPageSelected,
    pageSelectionIndeterminate,
    canSelectAllMatching,
    isSelected,
    toggle,
    togglePage,
    selectAllMatching,
    clear,
  }
}
