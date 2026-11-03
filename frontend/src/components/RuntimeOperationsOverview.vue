<template>
  <div class="operations-overview">
    <UiSection title="任务处理" variant="outlined">
      <div class="operations-body">
        <div v-if="execution?.maintenance_reserved" class="maintenance-label">维护中</div>
        <div class="worker-usage"><span>执行占用</span><strong>{{ execution ? execution.running : '-' }} <small>/ {{ data.execution_concurrency ?? '-' }}</small></strong></div>
        <div class="worker-track" role="img" :aria-label="`执行占用 ${execution?.running ?? '未取得'} / ${data.execution_concurrency ?? '未取得'}`"><i :style="{ width: `${occupied}%` }" /></div>
        <dl class="execution-counts"><div><dt>等待</dt><dd>{{ execution?.pending ?? '-' }}</dd></div><div><dt>延后</dt><dd>{{ execution?.delayed ?? '-' }}</dd></div><div :class="{ warning: execution && execution.unknown > 0 }"><dt>待核验</dt><dd>{{ execution?.unknown ?? '-' }}</dd></div></dl>
        <div v-if="data.runtime.accounting_db" class="connection-usage"><span>{{ data.runtime.shared_pool ? '数据库连接' : '结算库连接' }}</span><strong>{{ data.runtime.accounting_db.in_use }} / {{ data.runtime.accounting_db.maximum || '不限' }}</strong><div class="worker-track"><i :style="{ width: `${poolPercent(data.runtime.accounting_db)}%` }" /></div></div>
        <div v-if="data.runtime.traffic_read_db && !data.runtime.shared_pool" class="connection-usage"><span>流量库连接</span><strong>{{ data.runtime.traffic_read_db.in_use }} / {{ data.runtime.traffic_read_db.maximum || '不限' }}</strong><div class="worker-track"><i :style="{ width: `${poolPercent(data.runtime.traffic_read_db)}%` }" /></div></div>
      </div>
    </UiSection>
    <UiSection as="section" title="待处理队列" variant="outlined" aria-label="业务待处理概览">
      <div class="operations-body queue-overview">
        <div class="queue-legend"><span><i class="pending" />等待</span><span><i class="running" />执行中</span><span><i class="delayed" />延后</span></div>
        <article v-for="queue in data.queues" :key="queue.id" class="queue-visual">
          <div class="queue-visual-heading"><UiButton variant="link" size="sm" @click="$emit('select', queue.id)">{{ queue.name }}<span aria-hidden="true"> →</span></UiButton><span v-if="queue.oldest_at" class="queue-age">最早 <TimeBadge :value="queue.oldest_at" /></span><span class="queue-breakdown"><b class="pending">{{ queue.pending }}</b><b class="running">{{ queue.running }}</b><b class="delayed">{{ queue.delayed }}</b></span></div>
          <div class="queue-track" role="img" :aria-label="`${queue.name}：等待 ${queue.pending}，执行中 ${queue.running}，延后 ${queue.delayed}`"><i class="pending" :style="{ width: `${width(queue.pending)}%` }" /><i class="running" :style="{ width: `${width(queue.running)}%` }" /><i class="delayed" :style="{ width: `${width(queue.delayed)}%` }" /></div>
          <div v-if="queue.failed || queue.stale" class="queue-anomalies">{{ [queue.stale ? `${queue.stale} 项待接管` : '', queue.failed ? `${queue.failed} 项失败记录` : ''].filter(Boolean).join(' · ') }}</div>
        </article>
        <span v-if="!data.queues.length" class="unavailable">{{ queuesLoading ? '正在读取…' : '队列状态未取得' }}</span>
        <div class="spool-line"><span>流量事件入账</span><strong v-if="data.runtime.event_spool">{{ data.runtime.event_spool.pending_events }} <small>· {{ formatBytes(data.runtime.event_spool.pending_bytes) }}</small></strong><span v-else class="unavailable">{{ diagnosticsUnavailable ? '状态未取得' : '未启用' }}</span></div>
      </div>
    </UiSection>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import type { RuntimeJobs, DatabasePool } from '../api/runtimeJobs'
import { formatBytes } from '../api/systemMetrics'
import UiSection from './UiSection.vue'
import UiButton from './UiButton.vue'
import TimeBadge from './TimeBadge.vue'
const props = defineProps<{ data: Partial<RuntimeJobs> & Pick<RuntimeJobs, 'queues' | 'runtime'>; executionUnavailable: boolean; diagnosticsUnavailable: boolean; queuesLoading?: boolean }>()
defineEmits<{ select: [id: string] }>()
const execution = computed(() => props.executionUnavailable ? undefined : props.data.execution_queue)
const occupied = computed(() => Math.min(100, (execution.value?.running || 0) * 100 / (props.data.execution_concurrency || 1)))
const maxQueue = computed(() => Math.max(1, ...props.data.queues.map(q => q.pending + q.running + q.delayed)))
function width(n: number) { return n * 100 / maxQueue.value }
function poolPercent(pool: DatabasePool) { return pool.maximum > 0 ? Math.min(100, pool.in_use * 100 / pool.maximum) : 0 }
</script>
<style scoped>
.operations-overview{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1.8fr);gap:16px}.operations-body{padding:18px 20px}.worker-usage{display:flex;align-items:baseline;justify-content:space-between;font-size:13px;color:var(--muted)}.worker-usage strong{font-size:28px;color:var(--text-strong);font-weight:600;line-height:1}.worker-usage small{font-size:14px;color:var(--muted);font-weight:400}.worker-track,.queue-track{display:flex;height:6px;border-radius:5px;background:var(--surface-neutral);overflow:hidden}.worker-track{margin-top:14px}.worker-track i{background:var(--primary);height:100%;border-radius:5px}.execution-counts{display:grid;grid-template-columns:repeat(3,1fr);gap:12px;margin:22px 0}.execution-counts dt{font-size:12px;color:var(--muted)}.execution-counts dd{margin:5px 0 0;font-size:22px;font-weight:600}.warning dd{color:var(--warning)}.connection-usage{display:grid;grid-template-columns:1fr auto;gap:4px;font-size:12px;color:var(--muted);border-top:1px solid var(--line);padding-top:16px;margin-top:16px}.connection-usage strong{color:var(--text);font-weight:500}.connection-usage .worker-track{grid-column:1/-1;margin-top:8px}.queue-overview{display:grid;gap:16px}.queue-legend{display:flex;gap:16px;color:var(--muted);font-size:11px}.queue-legend span{display:flex;align-items:center;gap:5px}.queue-legend i{width:6px;height:6px;border-radius:50%}.pending{background:var(--primary)}.running{background:var(--success)}.delayed{background:var(--warning)}.queue-visual-heading{display:flex;gap:8px;align-items:center;margin-bottom:8px;font-size:12px}.queue-breakdown{margin-left:auto;display:flex;gap:10px;font-variant-numeric:tabular-nums}.queue-breakdown b{background:none;font-weight:500}.queue-breakdown .pending{color:var(--primary)}.queue-breakdown .running{color:var(--success)}.queue-breakdown .delayed{color:var(--warning)}.maintenance-label{color:var(--warning);font-size:12px;margin-bottom:12px}.queue-age{display:flex;gap:4px;align-items:center;color:var(--muted);font-size:11px}.queue-anomalies{color:var(--warning);font-size:11px;margin-top:6px}.spool-line{display:flex;justify-content:space-between;align-items:center;gap:12px;padding-top:14px;border-top:1px solid var(--line);font-size:12px}.spool-line small{font-weight:400;color:var(--muted)}.unavailable{color:var(--muted);font-size:12px}@media(max-width:1100px){.operations-overview{grid-template-columns:1fr 1fr}}@media(max-width:760px){.operations-overview{grid-template-columns:1fr}.operations-body{padding:16px}.queue-age{display:none}}
</style>
