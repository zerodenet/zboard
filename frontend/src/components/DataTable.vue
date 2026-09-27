<template>
  <div class="data-table-container">
    <div v-if="hasSecondaryColumns" class="table-column-controls">
      <button class="table-column-toggle" type="button" :aria-expanded="showSecondary" @click="showSecondary = !showSecondary">{{ showSecondary ? '只看主要列' : '显示更多列' }}</button>
    </div>
    <div class="table-shell data-table-shell" tabindex="0" role="region" :aria-label="caption">
      <table
        ref="table"
        class="data-table"
        :class="tableClass"
        :style="tableStyle"
        :data-density="density"
        :data-selectable="selectable ? 'true' : 'false'"
        :data-show-secondary="showSecondary ? 'true' : 'false'"
        :aria-rowcount="rowCount"
      >
        <caption :class="captionVisible ? 'data-table-caption' : 'sr-only'">{{ caption }}</caption>
        <slot v-if="$slots.default" />
        <template v-else-if="columns?.length">
          <thead><tr>
            <th v-for="column in columns" :key="column.key" :class="columnClass(column)" :data-column-priority="column.priority" :style="column.width ? { width: column.width } : undefined" :scope="'col'">
              <slot :name="`header-${column.key}`" :column="column">{{ column.label }}</slot>
            </th>
          </tr></thead>
          <tbody>
            <template v-if="loading">
              <tr v-for="index in skeletonRows" :key="`skeleton-${index}`" class="data-table-skeleton-row"><td v-for="column in columns" :key="column.key" :class="columnClass(column)" :data-column-priority="column.priority"><span class="data-table-skeleton-line" /></td></tr>
            </template>
            <template v-else-if="rows?.length">
              <tr v-for="(row, index) in rows" :key="getRowKey(row, index)">
                <td v-for="column in columns" :key="column.key" :class="columnClass(column)" :data-column-priority="column.priority">
                  <slot :name="`cell-${column.key}`" :row="row" :value="row[column.key]" :column="column" :index="index">{{ formatValue(row[column.key]) }}</slot>
                </td>
              </tr>
            </template>
            <tr v-else><td :colspan="columns.length" class="data-table-empty"><slot name="empty">暂无记录</slot></td></tr>
          </tbody>
        </template>
      </table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, onUpdated, ref } from 'vue'

export type DataTableColumn = {
  key: string
  label: string
  align?: 'left' | 'right' | 'center'
  width?: string
  priority?: 1 | 2 | 3
  primary?: boolean
  action?: boolean
}

const props = withDefaults(defineProps<{
  caption: string
  minWidth?: number | string
  density?: 'compact' | 'comfortable'
  selectable?: boolean
  rowCount?: number
  captionVisible?: boolean
  tableClass?: string
  columns?: DataTableColumn[]
  rows?: any[]
  rowKey?: string | ((row: Record<string, any>, index: number) => string | number)
  loading?: boolean
  skeletonRows?: number
}>(), {
  minWidth: 0,
  density: 'compact',
  selectable: false,
  rowCount: undefined,
  captionVisible: false,
  tableClass: '',
  columns: undefined,
  rows: undefined,
  rowKey: 'id',
  loading: false,
  skeletonRows: 5,
})

function columnClass(column: DataTableColumn) {
  return { 'numeric-column': column.align === 'right', 'table-primary-column': column.primary, 'table-action-column': column.action, 'data-table-center': column.align === 'center' }
}
function getRowKey(row: Record<string, any>, index: number) {
  return typeof props.rowKey === 'function' ? props.rowKey(row, index) : row[props.rowKey] ?? index
}
function formatValue(value: unknown) { return value == null || value === '' ? '—' : String(value) }

const emit = defineEmits<{ visibleColumnCount: [count: number] }>()
const table = ref<HTMLTableElement>()
const showSecondary = ref(false)
const hasSecondaryColumns = ref(false)
let lastColumnCount = 0
let tableResizeObserver: ResizeObserver | undefined

function updateColumns() {
  const headers = Array.from(table.value?.querySelectorAll('thead tr:first-child > th') || [])
  const availableWidth = table.value?.parentElement?.clientWidth || window.innerWidth
  const hidden = headers.filter(header => {
    const priority = header.getAttribute('data-column-priority')
    return priority === '3' && availableWidth <= 1100 || priority === '2' && availableWidth <= 720
  }).length
  hasSecondaryColumns.value = hidden > 0
  const count = headers.length - (showSecondary.value ? 0 : hidden)
  if (count !== lastColumnCount) { lastColumnCount = count; emit('visibleColumnCount', count) }
}
onMounted(() => {
  updateColumns()
  window.addEventListener('resize', updateColumns)
  if (typeof ResizeObserver !== 'undefined' && table.value?.parentElement) {
    tableResizeObserver = new ResizeObserver(updateColumns)
    tableResizeObserver.observe(table.value.parentElement)
  }
})
onUpdated(updateColumns)
onBeforeUnmount(() => { window.removeEventListener('resize', updateColumns); tableResizeObserver?.disconnect() })

const tableStyle = computed(() => {
  if (hasSecondaryColumns.value && !showSecondary.value) return { minWidth: '100%' }
  if (!props.minWidth) return undefined
  return { minWidth: typeof props.minWidth === 'number' ? `${props.minWidth}px` : props.minWidth }
})
</script>
