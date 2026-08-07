<template>
  <section class="standard-page">
    <PageHeader title="流量与对账" description="按真实业务对象筛选上报记录，在服务端分页和聚合，避免随数据量增长而加载全量用户、节点或端点。" eyebrow="Usage Operations">
      <template #actions><PageRefreshButton label="刷新流量与对账" :loading="loading" @click="load" /></template>
    </PageHeader>
    <TransientFeedback :error="error" error-title="流量数据加载失败" />

    <UiMetricStrip class="traffic-summary">
      <MetricCard label="匹配记录" :value="formatNumber(recordTotal)" icon="activity" status="筛选结果" tone="info" meta="当前时间范围内的服务端总数" />
      <MetricCard label="原始流量" :value="formatBytes(recordAggregates.raw_bytes || 0)" icon="database" status="上报汇总" tone="info" meta="不受当前游标页影响" />
      <MetricCard label="计费流量" :value="formatBytes(recordAggregates.used_bytes || 0)" icon="billing" status="计费汇总" tone="info" icon-tone="warning" meta="已应用协议倍率" />
      <MetricCard label="关联订阅" :value="formatNumber(recordAggregates.subscription_count || 0)" icon="plans" status="覆盖范围" tone="info" icon-tone="success" :meta="`${formatNumber(recordAggregates.user_count || 0)} 个用户 · ${formatNumber(recordAggregates.node_count || 0)} 个节点`" />
    </UiMetricStrip>

    <DataWorkbench :total="recordTotal" :loading="recordLoading || referenceLoading" :refreshing="recordRefreshing">
      <template #filters>
        <WorkbenchFilterBar :active="hasFilters" :loading="loading" @clear="clearFilters">
          <WorkbenchFilterInput v-model="filters.userId" label="用户 ID" value-prefix="#" inputmode="numeric" @apply="applyFilters" />
          <WorkbenchFilterInput v-model="filters.nodeId" label="节点 ID" value-prefix="#" inputmode="numeric" @apply="applyFilters" />
          <WorkbenchFilterInput v-model="filters.endpointId" label="协议端点 ID" value-prefix="#" inputmode="numeric" @apply="applyFilters" />
          <WorkbenchFilterInput v-model="filters.subscriptionId" label="订阅 ID" value-prefix="#" inputmode="numeric" @apply="applyFilters" />
          <WorkbenchFilterDate v-model:from="from" v-model:to="to" label="记录日期" @apply="applyFilters" />
        </WorkbenchFilterBar>
      </template>
      <template #actions><span class="workbench-note">内部 ID 仅用于筛选和辅助定位，列表以业务名称为主</span></template>

      <DataTable v-if="records.length" caption="流量上报记录列表" :row-count="recordTotal" :min-width="1120" table-class="traffic-table">
        <thead>
          <tr>
            <th class="table-primary-column">上报时间</th>
            <th data-column-priority="3">用户</th>
            <th>订阅</th>
            <th data-column-priority="2">节点</th>
            <th data-column-priority="2">协议端点</th>
            <th class="numeric-column" data-column-priority="3">原始流量</th>
            <th class="numeric-column" data-column-priority="3">倍率 (×)</th>
            <th class="numeric-column">计费流量</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="record in records" :key="record.id">
            <td class="table-primary-column"><TimeBadge :value="record.record_at" /></td>
            <td data-column-priority="3"><EntityReference :reference="userReference(record.user_id)" :fallback-id="record.user_id" fallback-kind="user" compact /></td>
            <td><EntityReference :reference="subscriptionReference(record.subscription_id)" :fallback-id="record.subscription_id" fallback-kind="subscription" compact /></td>
            <td data-column-priority="2"><EntityReference :reference="nodeReference(record.node_id)" :fallback-id="record.node_id" fallback-kind="node" compact /></td>
            <td data-column-priority="2"><EntityReference :reference="endpointReference(record.protocol_endpoint_id)" :fallback-id="record.protocol_endpoint_id" fallback-kind="protocol_endpoint" compact /></td>
            <td class="numeric-column" data-column-priority="3">{{ formatBytes(record.raw_bytes) }}</td>
            <td class="numeric-column" data-column-priority="3">{{ formatMultiplier(record.protocol_multiplier_milli) }}</td>
            <td class="numeric-column"><strong>{{ formatBytes(record.used_bytes) }}</strong></td>
          </tr>
        </tbody>
      </DataTable>
      <EmptyState v-else icon="activity" title="没有流量记录" description="当前筛选范围内尚未收到节点上报。" />
      <template #footer><CursorPager :count="records.length" :total="recordTotal" :limit="recordLimit" :loading="recordLoading" :has-previous="Boolean(recordPreviousCursor)" :has-next="Boolean(recordNextCursor)" @previous="changeRecordCursor(recordPreviousCursor)" @next="changeRecordCursor(recordNextCursor)" @limit="changeRecordLimit" /></template>
    </DataWorkbench>

    <UiMetricStrip class="reconciliation-summary">
      <MetricCard label="对账订阅" :value="formatNumber(reconciliationAggregates.subscription_count || 0)" icon="plans" status="完整范围" tone="info" :meta="`订阅 ${formatBytes(reconciliationAggregates.flow_used || 0)} · 记录 ${formatBytes(reconciliationAggregates.recorded_bytes || 0)}`" />
      <MetricCard label="一致" :value="formatNumber(reconciliationAggregates.matched_count || 0)" icon="check" status="已对平" tone="success" icon-tone="success" meta="订阅累计与记录汇总完全一致" />
      <MetricCard label="缺少记录" :value="formatNumber(reconciliationAggregates.missing_records_count || 0)" icon="alert" status="需核查" tone="warning" icon-tone="warning" :meta="`差额 ${formatBytes(reconciliationAggregates.missing_bytes || 0)}`" />
      <MetricCard label="记录超额" :value="formatNumber(reconciliationAggregates.over_recorded_count || 0)" icon="alert" status="需核查" tone="danger" icon-tone="danger" :meta="`超额 ${formatBytes(reconciliationAggregates.over_recorded_bytes || 0)}`" />
    </UiMetricStrip>

    <DataWorkbench class="reconciliation-workbench" :total="reconciliationTotal" :loading="reconciliationLoading || referenceLoading" :refreshing="reconciliationRefreshing">
      <template #filters><WorkbenchFilterBar :active="reconciliationMode !== 'issues'" @clear="reconciliationMode = 'issues'; applyReconciliationMode()"><WorkbenchFilterSelect v-model="reconciliationMode" label="对账范围" :options="reconciliationOptions" empty-value="issues" @apply="applyReconciliationMode" /></WorkbenchFilterBar></template>
      <template #actions><span class="workbench-note">汇总不受“仅异常/全部结果”和当前页影响；节点、端点、日期只作用于上方记录表</span></template>
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
      <EmptyState v-else icon="shield" :title="reconciliationMode === 'issues' ? '当前没有对账异常' : '没有对账结果'" description="订阅累计用量与上报记录汇总一致。" />
      <template #footer><TablePager :total="reconciliationTotal" :offset="reconciliationOffset" :limit="reconciliationLimit" :loading="reconciliationLoading" @change="changeReconciliationPage" /></template>
    </DataWorkbench>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { fetchTrafficReconciliationPage, fetchTrafficRecordsPage, type TrafficReconciliationAggregates, type TrafficReconciliationItem, type TrafficRecordAggregates, type TrafficRecordSummary } from '../api/client'
import { emptyEntityReferenceResponse, fetchAdminEntityReferences, type EntityReferenceResponse } from '../api/readModels'
import CursorPager from '../components/CursorPager.vue'
import DataTable from '../components/DataTable.vue'
import DataWorkbench from '../components/DataWorkbench.vue'
import EmptyState from '../components/EmptyState.vue'
import EntityReference from '../components/EntityReference.vue'
import MetricCard from '../components/MetricCard.vue'
import PageHeader from '../components/PageHeader.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TablePager from '../components/TablePager.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import WorkbenchFilterBar from '../components/WorkbenchFilterBar.vue'
import WorkbenchFilterDate from '../components/WorkbenchFilterDate.vue'
import WorkbenchFilterInput from '../components/WorkbenchFilterInput.vue'
import WorkbenchFilterSelect from '../components/WorkbenchFilterSelect.vue'
import UiMetricStrip from '../components/UiMetricStrip.vue'
import { resolveHistoryRange } from '../composables/historyState'
import { useCursorTable } from '../composables/useCursorTable'
import { useRemoteTable } from '../composables/useRemoteTable'
import { formatBytes, formatNumber, formatSignedBytes, formatUnknownValue } from '../utils/format'
import { preserveAdminReturnTo, withAdminReturnTo } from '../utils/navigation'

const route = useRoute()
const router = useRouter()
const filters = reactive({ userId: String(route.query.user_id || ''), nodeId: String(route.query.node_id || ''), endpointId: String(route.query.protocol_endpoint_id || ''), subscriptionId: String(route.query.subscription_id || '') })
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
const references = ref<EntityReferenceResponse>(emptyEntityReferenceResponse())
const referenceLoading = ref(false)
const referenceError = ref('')
const hasFilters = computed(() => Boolean(filters.userId || filters.nodeId || filters.endpointId || filters.subscriptionId))
const reconciliationOptions = [{ label: '仅异常', value: 'issues' }, { label: '全部结果', value: 'all' }]
const { items: records, total: recordTotal, aggregates: recordAggregates, nextCursor: recordNextCursor, previousCursor: recordPreviousCursor, loading: recordLoading, refreshing: recordRefreshing, error: recordError, load: loadRecords } = useCursorTable<TrafficRecordSummary, TrafficRecordAggregates>({
  fetchPage: ({ signal }) => fetchTrafficRecordsPage({ ...queryParams(), cursor: recordCursor.value || undefined, from: from.value, to: to.value, limit: recordLimit.value }, { signal }),
  errorMessage: (cause: any) => cause?.response?.data?.message || '流量记录加载失败。',
})
const { items: reconciliation, total: reconciliationTotal, aggregates: reconciliationAggregates, loading: reconciliationLoading, refreshing: reconciliationRefreshing, error: reconciliationError, load: loadReconciliation } = useRemoteTable<TrafficReconciliationItem, TrafficReconciliationAggregates>({
  offset: reconciliationOffset,
  limit: reconciliationLimit,
  fetchPage: ({ signal }) => {
    const params = queryParams()
    return fetchTrafficReconciliationPage({ userId: params.userId, subscriptionId: params.subscriptionId, issuesOnly: reconciliationMode.value === 'issues', offset: reconciliationOffset.value, limit: reconciliationLimit.value }, { signal })
  },
  errorMessage: (cause: any) => cause?.response?.data?.message || '流量对账加载失败。',
  onOffsetCorrected: () => syncURL(true),
})
const loading = computed(() => recordLoading.value || reconciliationLoading.value || referenceLoading.value)
const error = computed(() => recordError.value || reconciliationError.value || referenceError.value)

function formatMultiplier(value: number) { return (Number(value || 1000) / 1000).toLocaleString('zh-CN', { minimumFractionDigits: 0, maximumFractionDigits: 3 }) }
function resultName(result: string) { return ({ matched: '一致', missing_records: '缺少记录', over_recorded: '记录超额', legacy: '历史数据' } as Record<string, string>)[result] || formatUnknownValue('结果', result) }
function resultTone(result: string): 'success' | 'warning' | 'danger' { return result === 'matched' ? 'success' : result === 'legacy' ? 'warning' : 'danger' }
function subscriptionStatusName(status: string) { return ({ active: '有效', expired: '已失效', canceled: '已取消' } as Record<string, string>)[status] || formatUnknownValue('状态', status) }
function queryParams() { return { userId: Number(filters.userId) || undefined, nodeId: Number(filters.nodeId) || undefined, protocolEndpointId: Number(filters.endpointId) || undefined, subscriptionId: Number(filters.subscriptionId) || undefined } }
function adminContextLink(path: string, query: Record<string, string>) { return withAdminReturnTo(path, route.fullPath, query) }
function userReference(id: number) { return references.value.users[String(id)] || null }
function subscriptionReference(id: number) { return references.value.subscriptions[String(id)] || null }
function nodeReference(id: number) { return references.value.nodes[String(id)] || null }
function endpointReference(id: number) { return references.value.protocol_endpoints[String(id)] || null }
function planReference(id: number) { return references.value.plans[String(id)] || null }

async function loadReferences() {
  referenceLoading.value = true
  referenceError.value = ''
  try {
    references.value = await fetchAdminEntityReferences({
      userIds: [...records.value.map(item => item.user_id), ...reconciliation.value.map(item => item.user_id)],
      subscriptionIds: [...records.value.map(item => item.subscription_id), ...reconciliation.value.map(item => item.subscription_id)],
      nodeIds: records.value.map(item => item.node_id),
      protocolEndpointIds: records.value.map(item => item.protocol_endpoint_id),
      planIds: reconciliation.value.map(item => item.plan_id),
    })
  } catch (cause: any) {
    references.value = emptyEntityReferenceResponse()
    referenceError.value = cause?.response?.data?.message || '关联对象名称加载失败。'
  } finally {
    referenceLoading.value = false
  }
}

async function load() { await Promise.all([loadRecords(), loadReconciliation()]); await loadReferences() }
async function syncURL(replace = false) {
  const reconciliationPage = Math.floor(reconciliationOffset.value / reconciliationLimit.value) + 1
  const location = { query: {
    ...preserveAdminReturnTo(route.query.return_to),
    ...(filters.userId ? { user_id: filters.userId } : {}), ...(filters.nodeId ? { node_id: filters.nodeId } : {}), ...(filters.endpointId ? { protocol_endpoint_id: filters.endpointId } : {}), ...(filters.subscriptionId ? { subscription_id: filters.subscriptionId } : {}),
    from: from.value, to: to.value, ...(recordCursor.value ? { cursor: recordCursor.value } : {}), ...(recordLimit.value !== 50 ? { limit: String(recordLimit.value) } : {}), ...(reconciliationMode.value === 'all' ? { reconciliation: 'all' } : {}), ...(reconciliationPage > 1 ? { reconciliation_page: String(reconciliationPage) } : {}), ...(reconciliationLimit.value !== 25 ? { reconciliation_limit: String(reconciliationLimit.value) } : {})
  } }
  await (replace ? router.replace(location) : router.push(location))
}
function normalizeRange() { const range = resolveHistoryRange({ from: from.value, to: to.value }, 7); from.value = range.from; to.value = range.to }
async function applyFilters() { normalizeRange(); recordCursor.value = ''; reconciliationOffset.value = 0; await syncURL(); await load() }
async function clearFilters() { Object.assign(filters, { userId: '', nodeId: '', endpointId: '', subscriptionId: '' }); recordCursor.value = ''; reconciliationOffset.value = 0; await syncURL(); await load() }
async function applyReconciliationMode() { reconciliationOffset.value = 0; await syncURL(); await loadReconciliation(); await loadReferences() }
async function changeRecordCursor(value: string | null) { if (!value) return; recordCursor.value = value; await syncURL(); await loadRecords(); await loadReferences() }
async function changeRecordLimit(value: number) { recordLimit.value = allowedPageSizes.includes(value) ? value : 50; recordCursor.value = ''; await syncURL(); await loadRecords(); await loadReferences() }
async function changeReconciliationPage(value: { offset: number; limit: number }) { reconciliationOffset.value = value.offset; reconciliationLimit.value = value.limit; await syncURL(); await loadReconciliation(); await loadReferences() }
watch(() => route.fullPath, async () => {
  const nextFilters = { userId: String(route.query.user_id || ''), nodeId: String(route.query.node_id || ''), endpointId: String(route.query.protocol_endpoint_id || ''), subscriptionId: String(route.query.subscription_id || '') }
  const rawRecordLimit = Number(route.query.limit), nextRecordLimit = allowedPageSizes.includes(rawRecordLimit) ? rawRecordLimit : 50, nextRecordCursor = String(route.query.cursor || ''), nextRange = resolveHistoryRange(route.query, 7)
  const rawReconciliationLimit = Number(route.query.reconciliation_limit), nextReconciliationLimit = allowedPageSizes.includes(rawReconciliationLimit) ? rawReconciliationLimit : 25, nextReconciliationOffset = (Math.max(1, Number(route.query.reconciliation_page) || 1) - 1) * nextReconciliationLimit
  const nextMode = route.query.reconciliation === 'all' ? 'all' : 'issues'
  if (Object.keys(nextFilters).some(key => nextFilters[key as keyof typeof nextFilters] !== filters[key as keyof typeof filters]) || nextRecordLimit !== recordLimit.value || nextRecordCursor !== recordCursor.value || nextRange.from !== from.value || nextRange.to !== to.value || nextReconciliationLimit !== reconciliationLimit.value || nextReconciliationOffset !== reconciliationOffset.value || nextMode !== reconciliationMode.value) {
    Object.assign(filters, nextFilters); recordLimit.value = nextRecordLimit; recordCursor.value = nextRecordCursor; from.value = nextRange.from; to.value = nextRange.to; reconciliationLimit.value = nextReconciliationLimit; reconciliationOffset.value = nextReconciliationOffset; reconciliationMode.value = nextMode; await load()
  }
})
onMounted(async () => { if (!route.query.from || !route.query.to || route.query.page) await syncURL(true); await load() })
</script>

<style scoped>
.page-alert { margin-bottom: 16px; }
.traffic-summary { margin-bottom: 16px; }
.reconciliation-summary { margin-top: 16px; }
.reconciliation-workbench { margin-top: 12px; }
.workbench-note { color: var(--muted); font-size: 10px; }
.danger-text { color: var(--danger) !important; font-weight: 700; }
</style>
