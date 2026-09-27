<template>
  <nav class="table-pager data-pagination" :class="{ 'cursor-pager': mode === 'cursor' }" :data-variant="variant" :aria-label="ariaLabel">
    <span class="table-pager-summary" :class="{ 'cursor-pager-summary': mode === 'cursor' }">
      <slot name="summary" :first="firstItem" :last="lastItem" :total="total" :page="page" :page-count="pageCount">
        <template v-if="mode === 'cursor'">{{ count }} / {{ total ?? '—' }}</template>
        <template v-else-if="variant === 'stripe'">{{ firstItem }}–{{ lastItem }} / {{ total }}</template>
        <template v-else>第 {{ firstItem }}–{{ lastItem }} 条，共 {{ total }} 条</template>
      </slot>
    </span>
    <div class="table-pager-controls" :class="{ 'cursor-pager-controls': mode === 'cursor' }">
      <slot name="before-controls" />
      <label class="table-pager-size" :class="{ 'cursor-pager-size': mode === 'cursor' }">
        <span :class="{ 'sr-only': variant === 'stripe' }">每页</span>
        <UiSelect :model-value="limit" :options="pageSizeOptions" :aria-label="mode === 'cursor' ? '每次加载条数' : '每页条数'" :disabled="loading" @update:model-value="changeLimit" />
      </label>
      <slot name="navigation" :previous="previous" :next="next" :page="page" :page-count="pageCount">
        <button class="table-pager-nav table-pager-previous" :class="{ 'cursor-pager-nav cursor-pager-newer': mode === 'cursor' }" type="button" :disabled="previousDisabled" :aria-label="mode === 'cursor' ? '较新记录' : '上一页'" :title="mode === 'cursor' ? '较新记录' : '上一页'" @click="previous"><UiIcon v-if="variant === 'stripe' || mode === 'cursor'" name="chevron" /><template v-else>上一页</template></button>
        <span v-if="mode === 'offset'" class="table-pager-page">{{ page }} / {{ pageCount }}</span>
        <button class="table-pager-nav table-pager-next" :class="{ 'cursor-pager-nav cursor-pager-older': mode === 'cursor' }" type="button" :disabled="nextDisabled" :aria-label="mode === 'cursor' ? '较旧记录' : '下一页'" :title="mode === 'cursor' ? '较旧记录' : '下一页'" @click="next"><UiIcon v-if="variant === 'stripe' || mode === 'cursor'" name="chevron" /><template v-else>下一页</template></button>
      </slot>
      <slot name="after-controls" />
    </div>
  </nav>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import UiIcon from './UiIcon.vue'
import UiSelect from './UiSelect.vue'

const props = withDefaults(defineProps<{
  mode?: 'offset' | 'cursor'
  total?: number | null
  offset?: number
  count?: number
  limit: number
  loading?: boolean
  hasPrevious?: boolean
  hasNext?: boolean
  pageSizes?: number[]
  variant?: 'default' | 'stripe'
  ariaLabel?: string
}>(), { mode: 'offset', total: undefined, offset: 0, count: 0, loading: false, hasPrevious: false, hasNext: false, pageSizes: () => [25, 50, 100], variant: 'stripe', ariaLabel: '列表分页' })
const emit = defineEmits<{ change: [value: { offset: number; limit: number }]; previous: []; next: []; limit: [value: number] }>()
const safeTotal = computed(() => props.total ?? 0)
const pageCount = computed(() => Math.max(1, Math.ceil(safeTotal.value / props.limit)))
const page = computed(() => Math.min(pageCount.value, Math.floor(props.offset / props.limit) + 1))
const firstItem = computed(() => safeTotal.value ? props.offset + 1 : 0)
const lastItem = computed(() => Math.min(safeTotal.value, props.offset + props.limit))
const previousDisabled = computed(() => props.loading || (props.mode === 'cursor' ? !props.hasPrevious : props.offset <= 0))
const nextDisabled = computed(() => props.loading || (props.mode === 'cursor' ? !props.hasNext : props.offset + props.limit >= safeTotal.value))
const pageSizeOptions = computed(() => props.pageSizes.map(value => ({ label: props.variant === 'stripe' ? `${value} 条/${props.mode === 'cursor' ? '次' : '页'}` : `${value} / 页`, value })))
function previous() { if (previousDisabled.value) return; if (props.mode === 'cursor') emit('previous'); else emit('change', { offset: Math.max(0, props.offset - props.limit), limit: props.limit }) }
function next() { if (nextDisabled.value) return; if (props.mode === 'cursor') emit('next'); else emit('change', { offset: props.offset + props.limit, limit: props.limit }) }
function changeLimit(value: number) { const limit = Number(value); if (props.mode === 'cursor') emit('limit', limit); else emit('change', { offset: 0, limit }) }
</script>

<style scoped>
.data-pagination { min-height: 44px; padding: 7px 2px; color: var(--muted-foreground); font-size: 12px; }
.data-pagination .table-pager-summary { font-variant-numeric: tabular-nums; }
.data-pagination .table-pager-controls { display: flex; align-items: center; gap: 3px; }
.data-pagination .table-pager-size { display: inline-flex; align-items: center; gap: 0; margin-right: 9px; white-space: nowrap; }
.data-pagination :deep(.table-pager-size .ui-select) { width: auto; min-width: 85px; min-height: 30px; height: 30px; padding: 0 5px; border: 0; border-radius: 5px; color: var(--text-secondary); background: transparent; box-shadow: none; font-size: 12px; }
.data-pagination :deep(.table-pager-size .ui-select:hover) { color: var(--foreground); background: var(--surface-subtle); }
.data-pagination .table-pager-page { min-width: 42px; order: 3; font-variant-numeric: tabular-nums; text-align: center; }
.data-pagination .table-pager-nav { width: 30px; min-width: 30px; min-height: 30px; display: inline-grid; place-items: center; padding: 0; border: 0; border-radius: 5px; color: var(--text-secondary); background: transparent; cursor: pointer; }
.data-pagination .table-pager-nav:hover:not(:disabled) { color: var(--foreground); background: var(--surface-subtle); }
.data-pagination .table-pager-nav:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px; }
.data-pagination .table-pager-nav:disabled { color: var(--subtle); opacity: .45; cursor: default; }
.data-pagination .table-pager-nav .ui-icon { width: 13px; height: 13px; }
.data-pagination .table-pager-previous { order: 2; }
.data-pagination .table-pager-previous .ui-icon { transform: rotate(180deg); }
.data-pagination .table-pager-next { order: 4; }
@media (max-width: 720px) { .data-pagination { align-items: center; flex-direction: row; } .data-pagination .table-pager-controls { justify-content: flex-end; } }
@media (max-width: 480px) { .data-pagination .table-pager-summary { display: none; } .data-pagination .table-pager-controls { width: 100%; } }
</style>
