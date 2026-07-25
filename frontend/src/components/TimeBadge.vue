<template>
  <time
    class="time-badge"
    :class="`time-badge-${tone}`"
    :datetime="dateTimeAttribute"
    :title="exactLabel"
    :aria-label="`${label}，精确时间 ${exactLabel}`"
  >
    <UiIcon name="clock" />
    <span>{{ label }}</span>
  </time>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { formatCompactDateTime, formatDateTime, formatExactDateTime, formatRelativeTime } from '../utils/format'
import UiIcon from './UiIcon.vue'

const props = withDefaults(defineProps<{
  value?: string | null
  mode?: 'compact' | 'relative' | 'exact'
  tone?: 'neutral' | 'success' | 'warning' | 'danger' | 'info'
}>(), {
  value: null,
  mode: 'compact',
  tone: 'neutral',
})

const validDate = computed(() => {
  if (!props.value) return null
  const date = new Date(props.value)
  return Number.isNaN(date.getTime()) ? null : date
})
const dateTimeAttribute = computed(() => validDate.value?.toISOString())
const exactLabel = computed(() => validDate.value ? formatExactDateTime(props.value) : props.value ? `无效时间（${props.value}）` : formatExactDateTime(props.value))
const label = computed(() => {
  if (props.mode === 'relative') return formatRelativeTime(props.value)
  if (props.mode === 'exact') return formatDateTime(props.value)
  return formatCompactDateTime(props.value)
})
</script>
