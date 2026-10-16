<template>
  <UiSection title="流量维度统计" description="按用户、节点、订阅或协议端点查看整个筛选区间的用量，支持继续筛选和查看明细。">
    <template #actions>
      <UiSelect v-model="dimension" class="breakdown-dimension" aria-label="统计维度" :options="dimensions" />
      <UiButton variant="ghost" size="sm" :loading="loading" @click="load">刷新统计</UiButton>
    </template>
    <div class="breakdown-body">
      <PageAlert v-if="error" tone="danger" title="维度统计加载失败">{{ error }} <UiButton variant="secondary" @click="load">重试统计</UiButton></PageAlert>
      <p v-if="loading" role="status">正在加载维度统计…</p>
      <template v-if="data && !error">
        <UiMetricStrip>
          <MetricCard label="上传流量" :value="formatBytes(data.totals.upload_bytes)" icon="upload" meta="整个筛选区间" />
          <MetricCard label="下载流量" :value="formatBytes(data.totals.download_bytes)" icon="download" meta="整个筛选区间" />
          <MetricCard label="原始计量" :value="formatBytes(data.totals.raw_bytes)" icon="activity" meta="按订阅计量方式计算" />
          <MetricCard label="计费流量" :value="formatBytes(data.totals.used_bytes)" icon="billing" meta="已应用计费倍率和配额上限" />
        </UiMetricStrip>
        <p class="breakdown-meta">{{ data.total.toLocaleString() }} 项{{ dimensionLabel }}汇总 · <TimeBadge :value="data.as_of" /> · 每 3 秒更新 · 汇总不受当前页影响</p>
        <DataTable v-if="data.items.length" :caption="`${dimensionLabel}流量汇总`" :min-width="740">
          <thead><tr><th>{{ dimensionLabel }}</th><th class="numeric-column">上传</th><th class="numeric-column">下载</th><th class="numeric-column">原始计量</th><th class="numeric-column">计费流量</th><th class="numeric-column">用量占比</th><th class="table-action-column">操作</th></tr></thead>
          <tbody><tr v-for="item in data.items" :key="item.id">
            <td><strong>{{ item.name || (item.id ? `${dimensionLabel} #${item.id}` : `未归属${dimensionLabel}`) }}</strong><p class="breakdown-secondary">{{ item.secondary || (item.id ? `#${item.id}` : '保留未归属用量') }}<span v-if="item.missing && item.id"> · 对象已不可用</span></p></td>
            <td class="numeric-column">{{ formatBytes(item.upload_bytes) }}</td><td class="numeric-column">{{ formatBytes(item.download_bytes) }}</td>
            <td class="numeric-column">{{ formatBytes(item.raw_bytes) }}</td><td class="numeric-column"><strong>{{ formatBytes(item.used_bytes) }}</strong></td>
            <td class="numeric-column">{{ data.totals.used_bytes ? (item.used_bytes / data.totals.used_bytes * 100).toFixed(1) : '0.0' }}%</td>
            <td class="table-action-column"><UiButton variant="ghost" size="sm" :disabled="!item.id" @click="emit('drilldown', dimension, item.id, false)">筛选统计</UiButton><UiButton variant="ghost" size="sm" :disabled="!item.id" @click="emit('drilldown', dimension, item.id, true)">用量明细</UiButton></td>
          </tr></tbody>
        </DataTable>
        <EmptyState v-else icon="activity" title="没有维度统计" description="当前筛选区间尚无已入账的用量。" />
        <TablePager :total="data.total" :offset="offset" :limit="limit" :loading="loading" @change="changePage" />
      </template>
    </div>
  </UiSection>
</template>
<script setup lang="ts">
import { computed, onMounted, onScopeDispose, ref, watch } from 'vue'
import { fetchTrafficBreakdown, type TrafficBreakdown, type TrafficDimension } from '../api/trafficUsage'
import { useRemoteResource } from '../composables/useRemoteResource'
import { formatBytes } from '../utils/format'
import UiSection from '../components/UiSection.vue'
import UiSelect from '../components/UiSelect.vue'
import UiButton from '../components/UiButton.vue'
import UiMetricStrip from '../components/UiMetricStrip.vue'
import MetricCard from '../components/MetricCard.vue'
import PageAlert from '../components/PageAlert.vue'
import DataTable from '../components/DataTable.vue'
import TablePager from '../components/TablePager.vue'
import EmptyState from '../components/EmptyState.vue'
import TimeBadge from '../components/TimeBadge.vue'
const props = defineProps<{ dimension: TrafficDimension; userId: string; nodeId: string; subscriptionId: string; protocolEndpointId: string; from: string; to: string; refreshToken: number }>()
const emit = defineEmits<{ drilldown: [dimension: TrafficDimension, id: number, details: boolean]; 'update:dimension': [value: TrafficDimension] }>()
const dimension = computed({ get: () => props.dimension, set: value => emit('update:dimension', value) })
const offset = ref(0), limit = ref(25)
const dimensions = [{ label: '按用户', value: 'user' }, { label: '按节点', value: 'node' }, { label: '按订阅', value: 'subscription' }, { label: '按协议端点', value: 'protocol_endpoint' }]
const dimensionLabel = computed(() => ({ user: '用户', node: '节点', subscription: '订阅', protocol_endpoint: '协议端点' })[dimension.value])
const resource = useRemoteResource<TrafficBreakdown | null>({
  initial: () => null,
  fetch: ({ signal }) => fetchTrafficBreakdown({ dimension: dimension.value, userId: props.userId || undefined, nodeId: props.nodeId || undefined, subscriptionId: props.subscriptionId || undefined, protocolEndpointId: props.protocolEndpointId || undefined, from: props.from, to: props.to, offset: offset.value, limit: limit.value }, true, { signal }),
  errorMessage: '无法加载当前维度统计，请重试。',
})
const { data, loading, error } = resource
async function load() { resource.reset(); await resource.load() }
async function changePage(page: { offset: number; limit: number }) { offset.value = page.offset; limit.value = page.limit; await load() }
watch(() => JSON.stringify([props.userId, props.nodeId, props.subscriptionId, props.protocolEndpointId, props.from, props.to, dimension.value]), () => { offset.value = 0; void load() }, { immediate: true })
watch(() => props.refreshToken, () => { void load() })
watch(() => data.value?.total, total => {
  if (total !== undefined && offset.value > 0 && offset.value >= total) {
    offset.value = Math.max(0, Math.ceil(total / limit.value) - 1) * limit.value
    void load()
  }
})
let poll: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  poll = setInterval(() => {
    if (document.visibilityState === 'visible' && !loading.value && !error.value) void resource.load()
  }, 3000)
})
onScopeDispose(() => { if (poll) clearInterval(poll) })
</script>
<style scoped>
.breakdown-body { padding: 16px; }
:deep(.breakdown-dimension) { width: 156px; flex: 0 0 156px; }
:deep(.ui-section-actions) { display: flex; align-items: center; gap: 8px; flex-wrap: nowrap; flex-shrink: 0; }
.breakdown-meta, .breakdown-secondary { font-size: 12px; color: var(--muted-foreground); }
.breakdown-meta { margin: 12px 0; }
.breakdown-secondary { margin: 4px 0 0; }
.table-action-column { white-space: nowrap; }
@media (max-width: 600px) {
  :deep(.panel-header) { align-items: flex-start; flex-wrap: wrap; }
}
</style>
