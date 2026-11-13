<template>
  <p v-if="history?.expired || history?.truncated" class="muted traffic-history-coverage" role="status">
    {{ history.expired ? '明细已过期，结算汇总保留' : `${history.from === history.retained_from ? '近 365 天 · ' : ''}${labels[history.bucket]}汇总${history.timezone ? ` · ${history.timezone}` : ''}` }}
  </p>
</template>
<script setup lang="ts">
import type { TrafficHistoryCoverage } from '../../api/trafficUsage'
defineProps<{ history?: TrafficHistoryCoverage }>()
const labels = { minute: '分钟', hour: '小时', day: '日' }
</script>
