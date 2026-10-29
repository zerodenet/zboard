<template>
  <div class="cell-title" aria-label="协议业务与交付状态">
    <StatusBadge :tone="isActive ? 'info' : 'neutral'">{{ isActive ? '已启用' : '已停用' }}</StatusBadge>
    <small>{{ delivery }}</small>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import StatusBadge from './StatusBadge.vue'

const props = defineProps<{
  isActive: boolean
  publication?: { status?: string } | null
  latestDeployment?: { status?: string } | null
}>()

// The administrative switch, a configuration result and a live observation
// are different facts. No heartbeat or missing queue is proof of a listener.
const delivery = computed(() => {
  const pending = props.publication?.status
  if (pending) {
    return ({
      queued: '配置交付待处理',
      running: '配置交付处理中',
      reconciling: '配置交付结果待核验',
      deferred: '配置交付受能力限制',
      failed: '配置交付失败，待恢复',
      suspended: '配置交付已暂停',
      connections_pending: '配置已应用；旧连接撤权待核验',
    } as Record<string, string>)[pending] || '配置交付状态未知'
  }
  switch (props.latestDeployment?.status) {
    case 'succeeded': return '最近配置发布已确认'
    case 'failed': return '最近配置发布失败'
    case 'queued': return '最近配置发布待处理'
    case 'running': return '最近配置发布处理中'
    case 'reconciling': return '最近配置发布结果待核验'
    case 'deferred': return '最近配置发布受能力限制'
    default: return '配置交付状态未知'
  }
})
</script>
