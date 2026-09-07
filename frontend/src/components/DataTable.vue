<template>
  <div class="data-table-container">
    <div v-if="hasSecondaryColumns" class="table-column-controls">
      <span>{{ showSecondary ? '辅助信息已展开' : '已收起辅助信息' }}</span>
      <UiButton variant="ghost" size="sm" type="button" :aria-expanded="showSecondary" @click="showSecondary = !showSecondary">{{ showSecondary ? '收起辅助列' : '显示辅助列' }}</UiButton>
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
        <slot />
      </table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, onUpdated, ref } from 'vue'
import UiButton from './UiButton.vue'

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

const emit = defineEmits<{ visibleColumnCount: [count: number] }>()
const table = ref<HTMLTableElement>()
const showSecondary = ref(false)
const hasSecondaryColumns = ref(false)
let lastColumnCount = 0

function updateColumns() {
  const headers = Array.from(table.value?.querySelectorAll('thead tr:first-child > th') || [])
  const hidden = headers.filter(header => {
    const priority = header.getAttribute('data-column-priority')
    return priority === '3' && window.innerWidth <= 1100 || priority === '2' && window.innerWidth <= 720
  }).length
  hasSecondaryColumns.value = hidden > 0
  const count = headers.length - (showSecondary.value ? 0 : hidden)
  if (count !== lastColumnCount) { lastColumnCount = count; emit('visibleColumnCount', count) }
}
onMounted(() => { updateColumns(); window.addEventListener('resize', updateColumns) })
onUpdated(updateColumns)
onBeforeUnmount(() => window.removeEventListener('resize', updateColumns))

const tableStyle = computed(() => {
  if (hasSecondaryColumns.value && !showSecondary.value) return { minWidth: '100%' }
  if (!props.minWidth) return undefined
  return { minWidth: typeof props.minWidth === 'number' ? `${props.minWidth}px` : props.minWidth }
})
</script>
