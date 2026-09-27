<template>
  <DataPagination mode="cursor" :count="count" :total="total" :limit="limit" :loading="loading" :has-previous="hasPrevious" :has-next="hasNext" :page-sizes="pageSizes" aria-label="历史记录分页" @previous="$emit('previous')" @next="$emit('next')" @limit="$emit('limit', $event)">
    <template v-if="$slots.summary" #summary="scope"><slot name="summary" v-bind="scope" /></template>
    <template v-if="$slots['before-controls']" #before-controls><slot name="before-controls" /></template>
    <template v-if="$slots.navigation" #navigation="scope"><slot name="navigation" v-bind="scope" /></template>
    <template v-if="$slots['after-controls']" #after-controls><slot name="after-controls" /></template>
  </DataPagination>
</template>

<script setup lang="ts">
import DataPagination from './DataPagination.vue'
withDefaults(defineProps<{ count: number; total?: number | null; limit: number; loading?: boolean; hasPrevious?: boolean; hasNext?: boolean; pageSizes?: number[] }>(), { loading: false, hasPrevious: false, hasNext: false, pageSizes: () => [25, 50, 100] })
defineEmits<{ previous: []; next: []; limit: [value: number] }>()
</script>
