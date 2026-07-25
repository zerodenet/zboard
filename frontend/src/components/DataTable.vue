<template>
  <div class="table-shell data-table-shell">
    <table
      class="data-table"
      :class="tableClass"
      :style="tableStyle"
      :data-density="density"
      :data-selectable="selectable ? 'true' : 'false'"
      :aria-rowcount="rowCount"
    >
      <caption :class="captionVisible ? 'data-table-caption' : 'sr-only'">{{ caption }}</caption>
      <slot />
    </table>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  caption: string
  minWidth?: number | string
  density?: 'compact' | 'comfortable'
  selectable?: boolean
  rowCount?: number
  captionVisible?: boolean
  tableClass?: string
}>(), {
  minWidth: 0,
  density: 'compact',
  selectable: false,
  rowCount: undefined,
  captionVisible: false,
  tableClass: '',
})

const tableStyle = computed(() => {
  if (!props.minWidth) return undefined
  return { minWidth: typeof props.minWidth === 'number' ? `${props.minWidth}px` : props.minWidth }
})
</script>
