<template>
  <div class="status-count-filters" role="group" :aria-label="label">
    <button
      v-for="item in items"
      :key="item.value || 'all'"
      type="button"
      class="status-count-filter"
      :data-tone="item.tone"
      :aria-pressed="value === item.value"
      :title="item.caption"
      :disabled="loading"
      @click="$emit('select', item.value)"
    >
      <span>{{ item.label }}</span>
      <strong>{{ loading ? '—' : formatNumber(item.count) }}</strong>
    </button>
  </div>
</template>

<script setup lang="ts">
import { formatNumber } from '../utils/format'

defineProps<{
  label: string
  value: string
  loading?: boolean
  items: ReadonlyArray<{ value: string; label: string; count: number; caption?: string; tone?: string }>
}>()
defineEmits<{ select: [value: string] }>()
</script>

<style scoped>
.status-count-filters { min-height: 32px; display: inline-flex; align-items: center; gap: 2px; padding-left: 10px; overflow-x: auto; border-left: 1px solid var(--border); }
.status-count-filter { min-height: 30px; display: inline-flex; align-items: center; gap: 6px; flex: 0 0 auto; padding: 0 7px; border: 1px solid transparent; border-radius: 6px; color: var(--muted-foreground); background: transparent; font: inherit; font-size: 11px; white-space: nowrap; cursor: pointer; }
.status-count-filter:hover { color: var(--foreground); background: var(--muted-surface); }
.status-count-filter[aria-pressed='true'] { border-color: var(--border); color: var(--foreground); background: var(--surface-subtle); }
.status-count-filter:focus-visible { outline: 2px solid var(--ring); outline-offset: -2px; }
.status-count-filter:disabled { opacity: .6; cursor: wait; }
.status-count-filter strong { color: var(--foreground); font-size: 12px; font-weight: 600; font-variant-numeric: tabular-nums; }
.status-count-filter[data-tone='danger'] strong { color: var(--destructive); }
</style>
