<template>
  <StatusOverviewItem
    icon="activity"
    label="统计观察"
    :status="sourceRecorded ? '来源已记录' : '来源待核对'"
    :tone="sourceRecorded ? 'info' : 'neutral'"
    :description="sourceRecorded ? '当前数值已保存对应的统计事件来源。' : '当前数值的统计事件来源尚未确认。'"
  >
    <template #description>
      <span>{{ sourceLabel }}</span>
      <span> · 面板接收 <TimeBadge v-if="receivedAt" :value="receivedAt" /><template v-else>时间未知</template></span>
    </template>
  </StatusOverviewItem>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { NodeCounterEvidence } from '../api/client'
import StatusOverviewItem from './StatusOverviewItem.vue'
import TimeBadge from './TimeBadge.vue'

const props = defineProps<{ observation?: Partial<NodeCounterEvidence> | null }>()
// A captured source is separate from online status, freshness or configuration.
const sourceRecorded = computed(() => props.observation?.known === true
  && props.observation.latest_source_known === true
  && props.observation.stats_source_known === true
  && props.observation.source === 'buffered_stats')
const sourceLabel = computed(() => ({
  buffered_stats: '统计事件',
  historical_stats: '历史统计',
  heartbeat: '心跳上报',
  legacy_connector: '兼容上报',
  deleted: '已删除资产',
} as Record<string, string>)[props.observation?.source || ''] || '来源未确认')
const receivedAt = computed(() => {
  const value = props.observation?.counter_received_at
  if (typeof value !== 'string' || !value || value.startsWith('0001-01-01') || !Number.isFinite(Date.parse(value))) return undefined
  return value
})
</script>
