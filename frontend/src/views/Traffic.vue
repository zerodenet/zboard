<template>
  <section class="standard-page">
    <PageHeader title="流量与对账" description="按真实业务对象查看按天、小时或分钟聚合的流量趋势、节点排行和计费明细；原始记录仍保留用于审计与对账。" eyebrow="Usage Operations">
      <template #actions><PageRefreshButton label="刷新流量与对账" :loading="loading" @click="load" /></template>
    </PageHeader>
    <TransientFeedback :error="referenceError" error-title="关联对象名称加载失败" />
    <UiButton v-if="referenceError" variant="secondary" @click="referenceResource.load()">重试关联名称</UiButton>

    <PageAlert v-if="statisticsError" tone="danger" title="流量区间统计加载失败">{{ statisticsError }} <UiButton variant="secondary" @click="recordStatistics.load()">重试区间统计</UiButton></PageAlert>
    <p v-else-if="statisticsLoading" role="status">正在计算流量区间统计…</p>
    <p v-if="statisticsReady && statisticsData" class="muted">区间统计时间：<TimeBadge :value="statisticsData.as_of" />（翻页不刷新统计）<UiButton variant="ghost" size="sm" :disabled="statisticsLoading" @click="recordStatistics.load()">刷新区间统计</UiButton></p>

    <UiMetricStrip class="traffic-summary">
      <MetricCard label="聚合明细" :value="statisticsReady ? formatNumber(recordTotal || 0) : '—'" icon="activity" status="筛选结果" tone="info" :meta="`当前时间范围内的${bucketLabel}聚合结果`" />
      <MetricCard label="原始流量" :value="statisticsReady ? formatBytes(recordAggregates.raw_bytes || 0) : '—'" icon="database" status="上报汇总" tone="info" meta="不受当前游标页影响" />
      <MetricCard label="计费流量" :value="statisticsReady ? formatBytes(recordAggregates.used_bytes || 0) : '—'" icon="billing" status="计费汇总" tone="info" icon-tone="warning" meta="已应用实际计费倍率" />
      <MetricCard label="关联订阅" :value="statisticsReady ? formatNumber(recordAggregates.subscription_count || 0) : '—'" icon="plans" status="覆盖范围" tone="info" icon-tone="success" :meta="statisticsReady ? `${formatNumber(recordAggregates.user_count || 0)} 个用户 · ${formatNumber(recordAggregates.node_count || 0)} 个节点` : '区间统计尚未就绪'" />
    </UiMetricStrip>

    <PageAlert v-if="trendError" tone="danger" title="流量趋势加载失败">{{ trendError }} <UiButton variant="secondary" @click="trendResource.load()">重试趋势</UiButton></PageAlert>
    <TrafficObservabilityChart v-else
      :points="trafficTrend.points"
      :loading="trendLoading || !trendLoaded"
      :truncated="trafficTrend.truncated"
      :record-count="trafficTrend.record_count"
      :peak-connections="trafficTrend.peak_connections"
    />

    <PageAlert v-if="nodeSeriesError" tone="danger" title="节点流量排行加载失败">{{ nodeSeriesError }} <UiButton variant="secondary" @click="nodeSeriesResource.load()">重试节点流量</UiButton></PageAlert>
    <NodeTrafficChart v-else :data="nodeSeries" :loading="nodeSeriesLoading || !nodeSeriesLoaded" show-ranking />

    <DataWorkbench :total="recordTotal" :loading="recordLoading" :refreshing="recordRefreshing">
      <template #filters>
        <WorkbenchFilterBar :active="hasFilters" :loading="recordLoading" @clear="clearFilters">
          <WorkbenchFilterSelect v-model="bucket" label="汇总粒度" :options="bucketOptions" @apply="applyFilters" />
          <WorkbenchFilterInput v-model="filters.userId" label="用户 ID" value-prefix="#" inputmode="numeric" @apply="applyFilters" />
          <WorkbenchFilterInput v-model="filters.nodeId" label="节点 ID" value-prefix="#" inputmode="numeric" @apply="applyFilters" />
          <WorkbenchFilterInput v-model="filters.subscriptionId" label="订阅 ID" value-prefix="#" inputmode="numeric" @apply="applyFilters" />
          <WorkbenchFilterDate v-model:from="from" v-model:to="to" label="记录日期" @apply="applyFilters" />
        </WorkbenchFilterBar>
      </template>
      <template #actions><div class="workbench-actions"><UiButton variant="ghost" size="sm" type="button" @click="showYesterday">查看昨日</UiButton><span class="workbench-note">默认跨全部用户按节点统计；用户筛选仅在需要下钻时使用</span></div></template>

      <PageAlert v-if="recordError" tone="danger" title="流量记录加载失败">{{ recordError }} <UiButton variant="secondary" @click="loadRecords()">重试记录</UiButton></PageAlert>
      <p v-if="recordLoading && !records.length" role="status">正在加载流量记录…</p>
      <DataTable v-if="records.length" :caption="`流量使用明细（按${bucketLabel}聚合）`" :row-count="recordTotal" :min-width="1020" table-class="traffic-table">
        <thead>
          <tr>
            <th class="table-primary-column">时间</th>
            <th data-column-priority="3">用户</th>
            <th>订阅</th>
            <th data-column-priority="2">节点</th>
            <th class="numeric-column" data-column-priority="3">原始流量</th>
            <th class="numeric-column" data-column-priority="3">倍率 (×)</th>
            <th class="numeric-column">计费流量</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="record in records" :key="record.id">
            <td class="table-primary-column"><TimeBadge :value="record.record_at" /></td>
            <td data-column-priority="3"><EntityReference :reference="userReference(record.user_id)" :fallback-id="record.user_id" fallback-kind="user" compact /></td>
            <td>
              <EntityReference
                v-if="record.subscription_id"
                :reference="subscriptionReference(record.subscription_id)"
                :fallback-id="record.subscription_id || 0"
                fallback-kind="subscription"
                compact
              />
              <span v-else>—</span>
            </td>
            <td data-column-priority="2"><EntityReference :reference="nodeReference(record.node_id)" :fallback-id="record.node_id" fallback-kind="node" compact /></td>
            <td class="numeric-column" data-column-priority="3">{{ formatBytes(record.raw_bytes) }}</td>
            <td class="numeric-column" data-column-priority="3">{{ formatMultiplier(record.protocol_multiplier_milli) }}</td>
            <td class="numeric-column"><strong>{{ formatBytes(record.used_bytes) }}</strong></td>
          </tr>
        </tbody>
      </DataTable>
      <EmptyState v-else-if="recordsLoaded && !recordLoading && !recordError" icon="activity" title="没有流量使用明细" description="当前筛选范围内没有可汇总的计费记录。" />
      <template #footer><CursorPager :count="records.length" :total="recordTotal" :limit="recordLimit" :loading="recordLoading" :has-previous="Boolean(recordPreviousCursor)" :has-next="Boolean(recordNextCursor)" @previous="changeRecordCursor(recordPreviousCursor)" @next="changeRecordCursor(recordNextCursor)" @limit="changeRecordLimit" /></template>
    </DataWorkbench>

    <UiMetricStrip class="reconciliation-summary">
      <MetricCard label="对账订阅" :value="reconciliationLoaded && !reconciliationError ? formatNumber(reconciliationAggregates.subscription_count || 0) : '—'" icon="plans" status="完整范围" tone="info" :meta="`订阅 ${formatBytes(reconciliationAggregates.flow_used || 0)} · 记录 ${formatBytes(reconciliationAggregates.recorded_bytes || 0)}`" />
      <MetricCard label="一致" :value="reconciliationLoaded && !reconciliationError ? formatNumber(reconciliationAggregates.matched_count || 0) : '—'" icon="check" status="已对平" tone="success" icon-tone="success" meta="订阅累计与记录汇总完全一致" />
      <MetricCard label="缺少记录" :value="reconciliationLoaded && !reconciliationError ? formatNumber(reconciliationAggregates.missing_records_count || 0) : '—'" icon="alert" status="需核查" tone="warning" icon-tone="warning" :meta="`差额 ${formatBytes(reconciliationAggregates.missing_bytes || 0)}`" />
      <MetricCard label="记录超额" :value="reconciliationLoaded && !reconciliationError ? formatNumber(reconciliationAggregates.over_recorded_count || 0) : '—'" icon="alert" status="需核查" tone="danger" icon-tone="danger" :meta="`超额 ${formatBytes(reconciliationAggregates.over_recorded_bytes || 0)}`" />
    </UiMetricStrip>

    <DataWorkbench class="reconciliation-workbench" :total="reconciliationLoaded ? reconciliationTotal : undefined" :loading="reconciliationLoading" :refreshing="reconciliationRefreshing">
      <template #filters><WorkbenchFilterBar :active="reconciliationMode !== 'issues'" @clear="reconciliationMode = 'issues'; applyReconciliationMode()"><WorkbenchFilterSelect v-model="reconciliationMode" label="对账范围" :options="reconciliationOptions" empty-value="issues" @apply="applyReconciliationMode" /></WorkbenchFilterBar></template>
      <template #actions><span class="workbench-note">汇总不受“仅异常/全部结果”和当前页影响；节点、日期和汇总粒度只作用于上方记录表</span></template>
      <PageAlert v-if="reconciliationError" tone="danger" title="流量对账加载失败">{{ reconciliationError }} <UiButton variant="secondary" @click="loadReconciliation()">重试对账</UiButton></PageAlert>
      <p v-if="reconciliationLoading && !reconciliation.length" role="status">正在加载对账结果…</p>
      <DataTable v-if="reconciliation.length" caption="订阅流量对账结果" :row-count="reconciliationTotal" :min-width="1200" table-class="reconciliation-table">
        <thead>
          <tr>
            <th class="table-primary-column">订阅</th>
            <th data-column-priority="3">用户</th>
            <th data-column-priority="3">套餐</th>
            <th>订阅状态</th>
            <th class="numeric-column" data-column-priority="2">订阅累计</th>
            <th class="numeric-column" data-column-priority="2">记录汇总</th>
            <th class="numeric-column">差异</th>
            <th>对账结果</th>
            <th class="table-action-column"><span class="sr-only">操作</span></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in reconciliation" :key="item.subscription_id">
            <td class="table-primary-column"><EntityReference :reference="subscriptionReference(item.subscription_id)" :fallback-id="item.subscription_id" fallback-kind="subscription" /></td>
            <td data-column-priority="3"><EntityReference :reference="userReference(item.user_id)" :fallback-id="item.user_id" fallback-kind="user" compact /></td>
            <td data-column-priority="3"><EntityReference :reference="planReference(item.plan_id)" :fallback-id="item.plan_id" fallback-kind="plan" compact /></td>
            <td><StatusBadge :tone="item.status === 'active' ? 'success' : 'neutral'">{{ subscriptionStatusName(item.status) }}</StatusBadge></td>
            <td class="numeric-column" data-column-priority="2">{{ formatBytes(item.flow_used) }}</td>
            <td class="numeric-column" data-column-priority="2">{{ formatBytes(item.recorded_bytes) }}</td>
            <td class="numeric-column" :class="{ 'danger-text': item.difference }">{{ formatSignedBytes(item.difference) }}</td>
            <td><StatusBadge :tone="resultTone(item.result)">{{ resultName(item.result) }}</StatusBadge></td>
            <td class="table-action-column"><RouterLink class="button button-ghost button-sm" :to="adminContextLink('/admin/subscriptions', { user_id: String(item.user_id), subscription: String(item.subscription_id) })">查看订阅</RouterLink></td>
          </tr>
        </tbody>
      </DataTable>
      <EmptyState v-else-if="reconciliationLoaded && !reconciliationLoading && !reconciliationError" icon="shield" :title="reconciliationMode === 'issues' ? '当前没有对账异常' : '没有对账结果'" description="订阅累计用量与上报记录汇总一致。" />
      <template #footer><TablePager :total="reconciliationTotal" :offset="reconciliationOffset" :limit="reconciliationLimit" :loading="reconciliationLoading" @change="changeReconciliationPage" /></template>
    </DataWorkbench>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { fetchTrafficReconciliationPage, type TrafficReconciliationAggregates, type TrafficReconciliationItem } from '../api/client'
import { fetchTrafficNodeSeries, type TrafficNodeSeries, type TrafficUsageBucket } from '../api/trafficUsage'
import { emptyEntityReferenceResponse, fetchAdminEntityReferences, fetchTrafficTrends, type EntityReferenceResponse, type TrafficTrendResult } from '../api/readModels'
import CursorPager from '../components/CursorPager.vue'
import DataTable from '../components/DataTable.vue'
import DataWorkbench from '../components/DataWorkbench.vue'
import EmptyState from '../components/EmptyState.vue'
import EntityReference from '../components/EntityReference.vue'
import MetricCard from '../components/MetricCard.vue'
import PageHeader from '../components/PageHeader.vue'
import PageRefreshButton from '../components/PageRefreshButton.vue'
import TimeBadge from '../components/TimeBadge.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TablePager from '../components/TablePager.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import PageAlert from '../components/PageAlert.vue'
import { useRemoteResource } from '../composables/useRemoteResource'
import { trafficNodeBucket } from '../utils/trafficNodeBucket'
import { keyedLoad } from '../composables/keyedLoad'
import WorkbenchFilterBar from '../components/WorkbenchFilterBar.vue'
import WorkbenchFilterDate from '../components/WorkbenchFilterDate.vue'
import WorkbenchFilterInput from '../components/WorkbenchFilterInput.vue'
import WorkbenchFilterSelect from '../components/WorkbenchFilterSelect.vue'
import UiMetricStrip from '../components/UiMetricStrip.vue'
import UiButton from '../components/UiButton.vue'
import NodeTrafficChart from './account/NodeTrafficChart.vue'
import TrafficObservabilityChart from './account/TrafficObservabilityChart.vue'
import { resolveHistoryRange } from '../composables/historyState'
import { useTrafficUsageTable } from '../composables/useTrafficUsageTable'
import { useRemoteTable } from '../composables/useRemoteTable'
import { formatBytes, formatNumber, formatSignedBytes, formatUnknownValue } from '../utils/format'
import { preserveAdminReturnTo, withAdminReturnTo } from '../utils/navigation'

const route = useRoute()
const router = useRouter()
const filters = reactive({ userId: String(route.query.user_id || ''), nodeId: String(route.query.node_id || ''), subscriptionId: String(route.query.subscription_id || '') })
const bucket = ref<TrafficUsageBucket>(normalizeBucket(route.query.bucket))
const bucketOptions = [{ label: '按天', value: 'day' }, { label: '按小时', value: 'hour' }, { label: '按分钟', value: 'minute' }]
const bucketLabel = computed(() => bucket.value === 'minute' ? '分钟' : bucket.value === 'day' ? '天' : '小时')
const allowedPageSizes = [25, 50, 100]
const initialRecordLimit = Number(route.query.limit)
const recordLimit = ref(allowedPageSizes.includes(initialRecordLimit) ? initialRecordLimit : 50)
const recordCursor = ref(String(route.query.cursor || ''))
const initialRange = resolveHistoryRange(route.query, 7)
const from = ref(initialRange.from)
const to = ref(initialRange.to)
const initialReconciliationLimit = Number(route.query.reconciliation_limit)
const reconciliationLimit = ref(allowedPageSizes.includes(initialReconciliationLimit) ? initialReconciliationLimit : 25)
const reconciliationOffset = ref((Math.max(1, Number(route.query.reconciliation_page) || 1) - 1) * reconciliationLimit.value)
const reconciliationMode = ref(route.query.reconciliation === 'all' ? 'all' : 'issues')
const referenceResource = useRemoteResource<EntityReferenceResponse>({
  initial: emptyEntityReferenceResponse,
  fetch: ({ signal }) => fetchAdminEntityReferences({
    userIds: [...records.value.map(item => item.user_id), ...reconciliation.value.map(item => item.user_id)],
    subscriptionIds: [...records.value.map(item => item.subscription_id), ...reconciliation.value.map(item => item.subscription_id)]
      .filter((id): id is number => Boolean(id)),
    nodeIds: records.value.map(item => item.node_id), planIds: reconciliation.value.map(item => item.plan_id), signal,
  }),
  errorMessage: '关联对象名称加载失败。',
})
const trendResource = useRemoteResource<TrafficTrendResult>({
  initial: emptyTrafficTrend,
  fetch: ({ signal }) => fetchTrafficTrends({ admin: true, ...queryParams(), from: from.value, to: to.value, signal }),
  errorMessage: '管理端流量趋势加载失败。',
})
const nodeSeriesResource = useRemoteResource<TrafficNodeSeries | null>({
  initial: () => null,
  fetch: ({ signal }) => fetchTrafficNodeSeries({ ...queryParams(), bucket: chartBucket(), from: from.value, to: to.value }, true, { signal }),
  errorMessage: '管理端节点流量排行加载失败。',
})
const { data: references, loading: referenceLoading, error: referenceError } = referenceResource
const { data: trafficTrend, loading: trendLoading, error: trendError, loaded: trendLoaded } = trendResource
const { data: nodeSeries, loading: nodeSeriesLoading, error: nodeSeriesError, loaded: nodeSeriesLoaded } = nodeSeriesResource
const hasFilters = computed(() => Boolean(filters.userId || filters.nodeId || filters.subscriptionId || bucket.value !== 'hour'))
const reconciliationOptions = [{ label: '仅异常', value: 'issues' }, { label: '全部结果', value: 'all' }]
const recordTable = useTrafficUsageTable(() => ({ ...queryParams(), bucket: bucket.value, cursor: recordCursor.value || undefined, from: from.value, to: to.value, limit: recordLimit.value }), true)
const reconciliationTable = useRemoteTable<TrafficReconciliationItem, TrafficReconciliationAggregates>({
  offset: reconciliationOffset,
  limit: reconciliationLimit,
  fetchPage: ({ signal }) => {
    const params = queryParams()
    return fetchTrafficReconciliationPage({ userId: params.userId, subscriptionId: params.subscriptionId, issuesOnly: reconciliationMode.value === 'issues', offset: reconciliationOffset.value, limit: reconciliationLimit.value }, { signal })
  },
  errorMessage: (cause: any) => cause?.response?.data?.message || '流量对账加载失败。',
  onOffsetCorrected: () => syncURL(true),
})
const { items: records, total: recordTotal, aggregates: recordAggregates, nextCursor: recordNextCursor,
  previousCursor: recordPreviousCursor, loading: recordLoading, refreshing: recordRefreshing,
  error: recordError, hasLoaded: recordsLoaded, load: loadRecords } = recordTable
const { items: reconciliation, total: reconciliationTotal, aggregates: reconciliationAggregates,
  loading: reconciliationLoading, refreshing: reconciliationRefreshing, error: reconciliationError,
  hasLoaded: reconciliationLoaded, load: loadReconciliation } = reconciliationTable
// Each completed page can resolve its names immediately, without waiting for
// the other table or charts; replacing either snapshot invalidates old labels.
watch([records, reconciliation], () => {
  referenceResource.reset()
  if (records.value.length || reconciliation.value.length) void referenceResource.load()
})
const { statistics: recordStatistics, statisticsReady } = recordTable
const { loading: statisticsLoading, error: statisticsError, data: statisticsData } = recordStatistics
const loading = computed(() => statisticsLoading.value || recordLoading.value || reconciliationLoading.value || referenceLoading.value || trendLoading.value || nodeSeriesLoading.value)

function formatMultiplier(value: number) { return (Number(value || 1000) / 1000).toLocaleString('zh-CN', { minimumFractionDigits: 0, maximumFractionDigits: 3 }) }
function resultName(result: string) { return ({ matched: '一致', missing_records: '缺少记录', over_recorded: '记录超额', legacy: '历史数据' } as Record<string, string>)[result] || formatUnknownValue('结果', result) }
function resultTone(result: string): 'success' | 'warning' | 'danger' { return result === 'matched' ? 'success' : result === 'legacy' ? 'warning' : 'danger' }
function subscriptionStatusName(status: string) { return ({ active: '有效', expired: '已失效', canceled: '已取消' } as Record<string, string>)[status] || formatUnknownValue('状态', status) }
function queryParams() { return { userId: filters.userId || undefined, nodeId: filters.nodeId || undefined, subscriptionId: filters.subscriptionId || undefined } }
function normalizeBucket(value: unknown): TrafficUsageBucket { return value === 'minute' || value === 'day' ? value : 'hour' }
function emptyTrafficTrend(): TrafficTrendResult { return { from: '', to: '', points: [], record_count: 0, connection_sample_count: 0, peak_connections: null, truncated: false, subscriptions: [], as_of: '' } }
function chartBucket(): TrafficUsageBucket {
  return trafficNodeBucket(bucket.value, from.value, to.value, Boolean(filters.nodeId))
}
function adminContextLink(path: string, query: Record<string, string>) { return withAdminReturnTo(path, route.fullPath, query) }
function userReference(id: number) { return references.value.users[String(id)] || null }
function subscriptionReference(id?: number) { return id ? references.value.subscriptions[String(id)] || null : null }
function nodeReference(id: number) { return references.value.nodes[String(id)] || null }
function planReference(id: number) { return references.value.plans[String(id)] || null }

const trendKey = () => JSON.stringify([queryParams(), from.value, to.value])
const loadRecordQuery = keyedLoad(() => JSON.stringify([trendKey(), bucket.value, recordCursor.value, recordLimit.value]), recordTable)
const loadReconciliationQuery = keyedLoad(() => JSON.stringify([filters.userId, filters.subscriptionId,
  reconciliationMode.value, reconciliationOffset.value, reconciliationLimit.value]), reconciliationTable)
const loadTrendQuery = keyedLoad(trendKey, trendResource)
const loadNodeQuery = keyedLoad(() => JSON.stringify([trendKey(), chartBucket()]), nodeSeriesResource)

async function loadChanged(force = false) {
  await Promise.all([loadRecordQuery(force), recordTable.loadStatistics(force), loadReconciliationQuery(force), loadTrendQuery(force), loadNodeQuery(force)])
}
async function load() { await loadChanged(true) }
async function syncURL(replace = false) {
  const reconciliationPage = Math.floor(reconciliationOffset.value / reconciliationLimit.value) + 1
  const location = { query: {
    ...preserveAdminReturnTo(route.query.return_to),
    ...(filters.userId ? { user_id: filters.userId } : {}), ...(filters.nodeId ? { node_id: filters.nodeId } : {}), ...(filters.subscriptionId ? { subscription_id: filters.subscriptionId } : {}),
    ...(bucket.value !== 'hour' ? { bucket: bucket.value } : {}),
    from: from.value, to: to.value, ...(recordCursor.value ? { cursor: recordCursor.value } : {}), ...(recordLimit.value !== 50 ? { limit: String(recordLimit.value) } : {}), ...(reconciliationMode.value === 'all' ? { reconciliation: 'all' } : {}), ...(reconciliationPage > 1 ? { reconciliation_page: String(reconciliationPage) } : {}), ...(reconciliationLimit.value !== 25 ? { reconciliation_limit: String(reconciliationLimit.value) } : {})
  } }
  await (replace ? router.replace(location) : router.push(location))
}
function normalizeRange() { const range = resolveHistoryRange({ from: from.value, to: to.value }, 7); from.value = range.from; to.value = range.to }
async function applyFilters() { normalizeRange(); recordCursor.value = ''; reconciliationOffset.value = 0; await syncURL(); await loadChanged() }
async function clearFilters() { Object.assign(filters, { userId: '', nodeId: '', subscriptionId: '' }); bucket.value = 'hour'; recordCursor.value = ''; reconciliationOffset.value = 0; await syncURL(); await loadChanged() }
async function showYesterday() {
  const yesterday = new Date()
  yesterday.setUTCDate(yesterday.getUTCDate() - 1)
  const date = yesterday.toISOString().slice(0, 10)
  from.value = date
  to.value = date
  bucket.value = 'day'
  recordCursor.value = ''
  reconciliationOffset.value = 0
  await syncURL()
  await loadChanged()
}
async function applyReconciliationMode() { reconciliationOffset.value = 0; await syncURL(); await loadChanged() }
async function changeRecordCursor(value: string | null) { if (!value) return; recordCursor.value = value; await syncURL(); await loadChanged() }
async function changeRecordLimit(value: number) { recordLimit.value = allowedPageSizes.includes(value) ? value : 50; recordCursor.value = ''; await syncURL(); await loadChanged() }
async function changeReconciliationPage(value: { offset: number; limit: number }) { reconciliationOffset.value = value.offset; reconciliationLimit.value = value.limit; await syncURL(); await loadChanged() }
watch(() => route.fullPath, async () => {
  const nextFilters = { userId: String(route.query.user_id || ''), nodeId: String(route.query.node_id || ''), subscriptionId: String(route.query.subscription_id || '') }
  const nextBucket: TrafficUsageBucket = normalizeBucket(route.query.bucket)
  const rawRecordLimit = Number(route.query.limit), nextRecordLimit = allowedPageSizes.includes(rawRecordLimit) ? rawRecordLimit : 50, nextRecordCursor = String(route.query.cursor || ''), nextRange = resolveHistoryRange(route.query, 7)
  const rawReconciliationLimit = Number(route.query.reconciliation_limit), nextReconciliationLimit = allowedPageSizes.includes(rawReconciliationLimit) ? rawReconciliationLimit : 25, nextReconciliationOffset = (Math.max(1, Number(route.query.reconciliation_page) || 1) - 1) * nextReconciliationLimit
  const nextMode = route.query.reconciliation === 'all' ? 'all' : 'issues'
  if (Object.keys(nextFilters).some(key => nextFilters[key as keyof typeof nextFilters] !== filters[key as keyof typeof filters]) || nextBucket !== bucket.value || nextRecordLimit !== recordLimit.value || nextRecordCursor !== recordCursor.value || nextRange.from !== from.value || nextRange.to !== to.value || nextReconciliationLimit !== reconciliationLimit.value || nextReconciliationOffset !== reconciliationOffset.value || nextMode !== reconciliationMode.value) {
    Object.assign(filters, nextFilters); bucket.value = nextBucket; recordLimit.value = nextRecordLimit; recordCursor.value = nextRecordCursor; from.value = nextRange.from; to.value = nextRange.to; reconciliationLimit.value = nextReconciliationLimit; reconciliationOffset.value = nextReconciliationOffset; reconciliationMode.value = nextMode; await loadChanged()
  }
})
onMounted(async () => { if (!route.query.from || !route.query.to || route.query.page) await syncURL(true); await load() })
</script>

<style scoped>
.page-alert { margin-bottom: 16px; }
.traffic-summary { margin-bottom: 16px; }
.traffic-observability + .node-traffic { margin-top: 12px; }
.node-traffic + .data-workbench { margin-top: 16px; }
.reconciliation-summary { margin-top: 16px; }
.reconciliation-workbench { margin-top: 12px; }
.workbench-note { color: var(--muted); font-size: 10px; }
.workbench-actions { display: flex; align-items: center; justify-content: flex-end; gap: 8px; }
.danger-text { color: var(--danger) !important; font-weight: 700; }
</style>
