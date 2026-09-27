<template>
  <div class="table-skeleton" role="status" :aria-label="label" :style="{ '--skeleton-columns': columns }">
    <div class="table-skeleton-header" aria-hidden="true">
      <Skeleton v-for="column in columns" :key="`head-${column}`" class="ui-skeleton" />
    </div>
    <div v-for="row in rows" :key="row" class="table-skeleton-row" aria-hidden="true">
      <Skeleton v-for="column in columns" :key="column" :class="['ui-skeleton', { 'table-skeleton-short': column % 3 === 0 }]" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { Skeleton } from './ui/skeleton'

withDefaults(defineProps<{ label?: string; columns?: number; rows?: number }>(), {
  label: '正在加载数据', columns: 6, rows: 5,
})
</script>

<style scoped>
.table-skeleton { min-width: 0; overflow: hidden; }
.table-skeleton-header, .table-skeleton-row {
  display: grid;
  grid-template-columns: repeat(var(--skeleton-columns), minmax(0, 1fr));
  align-items: center;
  gap: 20px;
  padding-inline: 16px;
}
.table-skeleton-header { height: 40px; border-bottom: 1px solid var(--border); background: var(--muted-surface); }
.table-skeleton-header :deep(.ui-skeleton) { width: 65%; height: 10px; background: var(--border); }
.table-skeleton-row { height: 44px; border-bottom: 1px solid var(--border); }
.table-skeleton-row :deep(.ui-skeleton) { width: 82%; }
.table-skeleton-row :deep(.table-skeleton-short) { width: 55%; }
@media (max-width: 720px) {
  .table-skeleton-header, .table-skeleton-row { grid-template-columns: repeat(4, minmax(0, 1fr)); }
  .table-skeleton-header > :nth-child(n+5), .table-skeleton-row > :nth-child(n+5) { display: none; }
}
</style>
