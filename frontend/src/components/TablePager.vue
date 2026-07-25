<template>
  <nav class="table-pager" :data-variant="variant" aria-label="列表分页">
    <span class="table-pager-summary">
      <template v-if="variant === 'stripe'">{{ firstItem }}–{{ lastItem }} / {{ total }}</template>
      <template v-else>第 {{ firstItem }}–{{ lastItem }} 条，共 {{ total }} 条</template>
    </span>
    <div class="table-pager-controls">
      <label class="table-pager-size"><span :class="{ 'sr-only': variant !== 'stripe' }">每页</span><UiSelect :model-value="limit" :options="pageSizeOptions" aria-label="每页条数" @update:model-value="changeLimit" /></label>
      <UiButton class="table-pager-nav table-pager-previous" variant="secondary" size="sm" :icon="variant === 'stripe'" type="button" :disabled="offset <= 0 || loading" aria-label="上一页" title="上一页" @click="go(offset - limit)"><UiIcon v-if="variant === 'stripe'" name="chevron" /><template v-else>上一页</template></UiButton>
      <span class="table-pager-page">{{ page }} / {{ pageCount }}</span>
      <UiButton class="table-pager-nav table-pager-next" variant="secondary" size="sm" :icon="variant === 'stripe'" type="button" :disabled="offset + limit >= total || loading" aria-label="下一页" title="下一页" @click="go(offset + limit)"><UiIcon v-if="variant === 'stripe'" name="chevron" /><template v-else>下一页</template></UiButton>
    </div>
  </nav>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'
import UiSelect from './UiSelect.vue'

const props = withDefaults(defineProps<{ total: number; offset: number; limit: number; loading?: boolean; pageSizes?: number[]; variant?: 'default' | 'stripe' }>(), {
  loading: false,
  pageSizes: () => [25, 50, 100],
  variant: 'stripe',
})
const pageSizeOptions = computed(() => props.pageSizes.map(value => ({ label: props.variant === 'stripe' ? String(value) : `${value} / 页`, value })))
const emit = defineEmits<{ change: [value: { offset: number; limit: number }] }>()
const pageCount = computed(() => Math.max(1, Math.ceil(props.total / props.limit)))
const page = computed(() => Math.min(pageCount.value, Math.floor(props.offset / props.limit) + 1))
const firstItem = computed(() => props.total ? props.offset + 1 : 0)
const lastItem = computed(() => Math.min(props.total, props.offset + props.limit))
function go(offset: number) { emit('change', { offset: Math.max(0, offset), limit: props.limit }) }
function changeLimit(limit: number) { emit('change', { offset: 0, limit: Number(limit) }) }
</script>

<style scoped>
.table-pager[data-variant='stripe'] {
  min-height: 42px;
  padding: 6px 12px;
  color: var(--muted);
  font-size: 10px;
}
.table-pager[data-variant='stripe'] .table-pager-summary {
  font-variant-numeric: tabular-nums;
}
.table-pager[data-variant='stripe'] .table-pager-controls {
  gap: 5px;
}
.table-pager[data-variant='stripe'] .table-pager-size {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin-right: 7px;
  color: var(--muted);
  white-space: nowrap;
}
.table-pager[data-variant='stripe'] :deep(.table-pager-size .p-select) {
  width: 62px;
  min-height: 28px;
  height: 28px;
  border-radius: 6px;
  box-shadow: none;
}
.table-pager[data-variant='stripe'] :deep(.table-pager-size .p-select-label) {
  padding: 5px 7px;
  font-size: 10px;
  line-height: 16px;
}
.table-pager[data-variant='stripe'] :deep(.table-pager-size .p-select-dropdown) {
  width: 22px;
}
.table-pager[data-variant='stripe'] .table-pager-page {
  min-width: 43px;
  order: 2;
  color: var(--text-secondary);
  font-variant-numeric: tabular-nums;
  text-align: center;
}
.table-pager[data-variant='stripe'] .table-pager-nav.p-button {
  width: 28px;
  min-width: 28px;
  min-height: 28px;
  height: 28px;
  padding: 0;
  border-color: var(--line);
  border-radius: 6px;
  color: var(--text-secondary);
  background: var(--surface);
  box-shadow: none;
}
.table-pager[data-variant='stripe'] .table-pager-nav.p-button:hover:not(:disabled) {
  border-color: var(--line-strong);
  color: var(--text-strong);
  background: var(--surface-hover);
}
.table-pager[data-variant='stripe'] .table-pager-nav.p-button:disabled {
  color: var(--subtle);
  opacity: .42;
}
.table-pager[data-variant='stripe'] .table-pager-nav .ui-icon {
  width: 12px;
  height: 12px;
}
.table-pager[data-variant='stripe'] .table-pager-previous {
  order: 3;
}
.table-pager[data-variant='stripe'] .table-pager-previous .ui-icon {
  transform: rotate(180deg);
}
.table-pager[data-variant='stripe'] .table-pager-next {
  order: 4;
}

@media (max-width: 720px) {
  .table-pager[data-variant='stripe'] {
    align-items: center;
    flex-direction: row;
  }
  .table-pager[data-variant='stripe'] .table-pager-controls {
    justify-content: flex-end;
  }
}

@media (max-width: 480px) {
  .table-pager[data-variant='stripe'] .table-pager-summary {
    display: none;
  }
  .table-pager[data-variant='stripe'] .table-pager-controls {
    width: 100%;
  }
}
</style>
