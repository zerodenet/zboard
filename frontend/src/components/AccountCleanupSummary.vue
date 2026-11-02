<template>
  <div class="account-cleanup-summary" :data-cleanup-state="status?.state || 'unknown'">
    <template v-if="!status"><span>清理状态未知</span></template>
    <template v-else-if="!status.enabled"><span>未启用</span></template>
    <template v-else-if="status.state === 'protected'"><span>暂不删除</span><small>{{ status.reason }}</small></template>
    <template v-else>
      <span v-if="status.state === 'eligible'">等待清理</span>
      <time v-else-if="status.eligible_at" :datetime="status.eligible_at">{{ formatDateTime(status.eligible_at) }}</time>
      <span v-else>长期订阅中</span>
      <small>{{ status.state === 'subscribed' ? `订阅结束 ${status.inactive_days} 天后` : status.state === 'eligible' ? '已达到自动删除条件' : `无订阅满 ${status.inactive_days} 天` }}</small>
    </template>
  </div>
</template>
<script setup lang="ts">
import type { AccountCleanupStatus } from '../api/client'
import { formatDateTime } from '../utils/format'
defineProps<{ status?: AccountCleanupStatus | null }>()
</script>
<style scoped>
.account-cleanup-summary { display: grid; gap: 2px; font-size: 13px; line-height: 1.4; }
.account-cleanup-summary time { white-space: nowrap; font-variant-numeric: tabular-nums; }
.account-cleanup-summary small { color: var(--muted-foreground); font-size: 12px; }
</style>
