<template>
  <span class="metric-comparison" :class="direction" :title="`${label}：${format(previous)}`">
    <span>{{ text }}</span><span class="comparison-period">{{ label }}</span>
  </span>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { formatNumber } from '../utils/format'
const props = withDefaults(defineProps<{ current: number; previous: number; label?: string; format?: (value: number) => string }>(), { label: '对比上期', format: formatNumber })
const direction = computed(() => props.current > props.previous ? 'increase' : props.current < props.previous ? 'decrease' : 'unchanged')
const text = computed(() => {
  if (!Number.isFinite(props.current) || !Number.isFinite(props.previous)) return '暂无可比数据'
  if (props.current === props.previous) return '— 持平'
  if (props.previous === 0) return `↑ +${props.format(props.current)}（上期为 0）`
  const percent = Math.abs((props.current - props.previous) / props.previous * 100).toFixed(1)
  return `${props.current > props.previous ? '↑' : '↓'} ${percent}%`
})
</script>
<style scoped>
.metric-comparison { display: inline-flex; flex-wrap: wrap; gap: 4px 8px; font-size: 12px; }
.increase { color: var(--success); }
.decrease { color: var(--destructive); }
.unchanged, .comparison-period { color: var(--muted-foreground); }
</style>
