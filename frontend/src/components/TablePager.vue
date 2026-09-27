<template>
  <DataPagination :total="total" :offset="offset" :limit="limit" :loading="loading" :page-sizes="pageSizes" :variant="variant" aria-label="列表分页" @change="$emit('change', $event)">
    <template v-if="$slots.summary" #summary="scope"><slot name="summary" v-bind="scope" /></template>
    <template v-if="$slots['before-controls']" #before-controls><slot name="before-controls" /></template>
    <template v-if="$slots.navigation" #navigation="scope"><slot name="navigation" v-bind="scope" /></template>
    <template v-if="$slots['after-controls']" #after-controls><slot name="after-controls" /></template>
  </DataPagination>
</template>

<script setup lang="ts">
import DataPagination from './DataPagination.vue'
withDefaults(defineProps<{ total: number; offset: number; limit: number; loading?: boolean; pageSizes?: number[]; variant?: 'default' | 'stripe' }>(), { loading: false, pageSizes: () => [25, 50, 100], variant: 'stripe' })
defineEmits<{ change: [value: { offset: number; limit: number }] }>()
</script>
