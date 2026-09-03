<template>
  <section class="account-page stack">
    <PageHeader title="流量明细" description="按时间范围查看使用趋势、节点流量分布和按分钟、小时或天聚合的计费明细。" eyebrow="TRAFFIC">
      <template #actions>
        <UiButton variant="secondary" type="button" :disabled="loading" @click="loadAll">
          <UiIcon name="refresh" />刷新
        </UiButton>
      </template>
    </PageHeader>

    <PageAlert v-if="summaryError" tone="danger" title="流量汇总加载失败">{{ summaryError }} <UiButton variant="secondary" @click="summaryResource.load()">重试汇总</UiButton></PageAlert>

    <UiMetricStrip :columns="4">
      <MetricCard
        label="剩余可用"
        :value="summaryLoaded && !summaryError ? formatBytes(summary.remaining_bytes) : '—'"
        icon="activity"
        :status="summaryLoading ? '加载中' : summaryError ? '读取失败' : '可用'"
        tone="info"
        meta="当前有效订阅汇总"
      />
      <MetricCard
        label="累计使用"
        :value="summaryLoaded && !summaryError ? formatBytes(summary.total_used_bytes) : '—'"
        icon="database"
        :status="summaryLoading ? '加载中' : summaryError ? '读取失败' : '历史'"
        tone="info"
        icon-tone="success"
        meta="历史计费流量"
      />
      <MetricCard
        label="今日使用"
        :value="summaryLoaded && !summaryError ? formatBytes(summary.used_bytes_today) : '—'"
        icon="clock"
        :status="summaryLoading ? '加载中' : summaryError ? '读取失败' : '今日'"
        tone="info"
        icon-tone="warning"
      >
        <template #meta><TimeBadge :value="summary.as_of" mode="relative" /></template>
      </MetricCard>
      <MetricCard
        label="最高连接数"
        :value="observabilityError || observability.peak_connections === null ? '—' : observability.peak_connections.toLocaleString('zh-CN')"
        icon="activity"
        :status="observabilityLoading ? '加载中' : observabilityError ? '读取失败' : observability.peak_connections === null ? '未采集' : '区间峰值'"
        tone="info"
        :meta="observabilityLoading ? '正在读取连接观测' : observabilityError ? '连接观测读取失败' : observability.peak_connections === null ? '当前范围暂无连接观测；旧版内核会保持未采集状态' : '取筛选范围内的最高并发值'"
      />
    </UiMetricStrip>

    <PageAlert v-if="observabilityError" tone="danger" title="流量趋势加载失败">{{ observabilityError }} <UiButton variant="secondary" @click="observabilityResource.load()">重试趋势</UiButton></PageAlert>
    <TrafficObservabilityChart v-else
      :points="observability.points"
      :loading="observabilityLoading || !observabilityLoaded"
      :truncated="observability.truncated"
      :record-count="observability.record_count"
      :peak-connections="observability.peak_connections"
    />

    <PageAlert v-if="nodeSeriesError" tone="danger" title="节点流量趋势加载失败">{{ nodeSeriesError }} <UiButton variant="secondary" @click="nodeSeriesResource.load()">重试节点流量</UiButton></PageAlert>
    <NodeTrafficChart v-else :data="nodeSeries" :loading="nodeSeriesLoading || !nodeSeriesLoaded" />

    <PageAlert v-if="statisticsError" tone="danger" title="流量区间统计加载失败">{{ statisticsError }} <UiButton variant="secondary" @click="recordStatistics.load()">重试区间统计</UiButton></PageAlert>
    <p v-else-if="statisticsLoading" role="status">正在计算流量区间统计…</p>
    <p v-if="statisticsReady && statisticsData" class="muted">区间统计时间：<TimeBadge :value="statisticsData.as_of" />（翻页不刷新统计）<UiButton variant="ghost" size="sm" :disabled="statisticsLoading" @click="recordStatistics.load()">刷新区间统计</UiButton></p>

    <DataWorkbench :total="total" :loading="recordLoading" :refreshing="refreshing">
      <template #filters>
        <WorkbenchFilterBar :active="Boolean(subscriptionFilter || bucket !== 'hour' || from || to)" @clear="clearFilters">
          <WorkbenchFilterSelect
            v-model="bucket"
            label="汇总粒度"
            :options="bucketOptions"
            @apply="applyFilters"
          />
          <AccountSubscriptionFilter v-model="subscriptionFilter" @apply="applyFilters" />
          <WorkbenchFilterDate v-model:from="from" v-model:to="to" label="记录日期" @apply="applyFilters" />
        </WorkbenchFilterBar>
      </template>
      <template #actions>
        <span v-if="statisticsReady" class="muted">范围内计费 {{ formatBytes(aggregates.used_bytes) }}</span>
      </template>

      <PageAlert v-if="recordError" tone="danger" title="流量记录加载失败">{{ recordError }} <UiButton variant="secondary" @click="loadRecords()">重试记录</UiButton></PageAlert>
      <p v-if="recordLoading && !records.length" role="status">正在加载流量记录…</p>
      <DataTable v-if="records.length" :caption="`我的流量使用明细（按${bucketLabel}聚合）`" :row-count="total" :min-width="920">
        <thead>
          <tr>
            <th class="table-primary-column">时间</th>
            <th>节点</th>
            <th>订阅</th>
            <th data-column-priority="2">上行</th>
            <th data-column-priority="2">下行</th>
            <th data-column-priority="3">倍率</th>
            <th>计费流量</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="record in records" :key="record.id">
            <td class="table-primary-column"><TimeBadge :value="record.record_at" /></td>
            <td>
              <EntityReference
                :reference="nodeReference(record.node_id)"
                :fallback-id="record.node_id"
                fallback-kind="node"
              />
            </td>
            <td>
              <EntityReference
                v-if="record.subscription_id"
                :reference="subscriptionReference(record.subscription_id)"
                :fallback-id="record.subscription_id"
                fallback-kind="subscription"
              />
              <span v-else>—</span>
            </td>
            <td class="numeric-column" data-column-priority="2">{{ formatBytes(record.upload_bytes) }}</td>
            <td class="numeric-column" data-column-priority="2">{{ formatBytes(record.download_bytes) }}</td>
            <td class="numeric-column" data-column-priority="3">{{ formatMultiplier(record.protocol_multiplier_milli) }}</td>
            <td class="numeric-column"><strong>{{ formatBytes(record.used_bytes) }}</strong></td>
          </tr>
        </tbody>
      </DataTable>
      <EmptyState
        v-else-if="recordsLoaded && !recordLoading && !recordError"
        icon="activity"
        title="还没有流量使用明细"
        :description="`节点开始产生计费记录后，按${bucketLabel}聚合的使用明细会显示在这里。`"
      />

      <template #footer>
        <CursorPager
          :count="records.length"
          :total="total"
          :limit="limit"
          :loading="recordLoading"
          :has-previous="Boolean(previousCursor)"
          :has-next="Boolean(nextCursor)"
          @previous="changeCursor(previousCursor)"
          @next="changeCursor(nextCursor)"
          @limit="changeLimit"
        />
      </template>
    </DataWorkbench>

    <aside class="account-notice">
      <UiIcon name="activity" />
      <div>
        <strong>流量明细如何计算？</strong>
        <p>明细由服务端按照时间粒度、节点、订阅和实际计费倍率汇总；原始计费记录仍保留用于计费、审计与对账。节点趋势直接从同一批原始计费事实按时间与节点计算，不额外生成统计记录。连接峰值由支持连接观测的内核上报绝对连接事实后，由 ZBoard 按发生时间重放并聚合，旧版内核没有观测时保持未采集。</p>
      </div>
    </aside>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { fetchTrafficSummary } from '../../api/client'
import AccountSubscriptionFilter from './AccountSubscriptionFilter.vue'
import {
  fetchTrafficNodeSeries,
  type TrafficNodeSeries,
  type TrafficUsageBucket,
} from '../../api/trafficUsage'
import type { EntityReference as EntityReferenceData } from '../../api/readModels'
import CursorPager from '../../components/CursorPager.vue'
import DataTable from '../../components/DataTable.vue'
import DataWorkbench from '../../components/DataWorkbench.vue'
import EmptyState from '../../components/EmptyState.vue'
import EntityReference from '../../components/EntityReference.vue'
import PageHeader from '../../components/PageHeader.vue'
import TimeBadge from '../../components/TimeBadge.vue'
import PageAlert from '../../components/PageAlert.vue'
import MetricCard from '../../components/MetricCard.vue'
import UiMetricStrip from '../../components/UiMetricStrip.vue'
import { useRemoteResource } from '../../composables/useRemoteResource'
import { trafficNodeBucket } from '../../utils/trafficNodeBucket'
import { keyedLoad } from '../../composables/keyedLoad'
import UiButton from '../../components/UiButton.vue'
import UiIcon from '../../components/UiIcon.vue'
import WorkbenchFilterBar from '../../components/WorkbenchFilterBar.vue'
import WorkbenchFilterDate from '../../components/WorkbenchFilterDate.vue'
import WorkbenchFilterSelect from '../../components/WorkbenchFilterSelect.vue'
import { resolveHistoryRange } from '../../composables/historyState'
import { useTrafficUsageTable } from '../../composables/useTrafficUsageTable'
import { formatBytes } from '../../utils/format'
import NodeTrafficChart from './NodeTrafficChart.vue'
import TrafficObservabilityChart from './TrafficObservabilityChart.vue'
import {
  emptyTrafficObservabilityResult,
  fetchTrafficObservability,
  type TrafficObservabilityResult,
} from './trafficObservability'

const route = useRoute()
const router = useRouter()
const allowedPageSizes = [25, 50, 100]
const initialLimit = Number(route.query.limit)
const limit = ref(allowedPageSizes.includes(initialLimit) ? initialLimit : 25)
const cursor = ref(String(route.query.cursor || ''))
const initialRange = resolveHistoryRange(route.query, 7)
const from = ref(initialRange.from)
const to = ref(initialRange.to)
const subscriptionFilter = ref(String(route.query.subscription_id || ''))
const bucket = ref<TrafficUsageBucket>(normalizeBucket(route.query.bucket))
const summaryResource = useRemoteResource<Record<string, any>>({
  initial: () => ({}), fetch: ({ signal }) => fetchTrafficSummary(false, { signal }), errorMessage: '流量汇总加载失败。',
})
const observabilityResource = useRemoteResource<TrafficObservabilityResult>({
  initial: emptyTrafficObservabilityResult,
  fetch: ({ signal }) => fetchTrafficObservability({
    subscriptionId: subscriptionFilter.value || undefined, from: from.value, to: to.value, signal,
  }),
  errorMessage: '流量趋势加载失败。',
})
const nodeSeriesResource = useRemoteResource<TrafficNodeSeries | null>({
  initial: () => null,
  fetch: ({ signal }) => fetchTrafficNodeSeries({
    bucket: chartBucket(), subscriptionId: subscriptionFilter.value || undefined, from: from.value, to: to.value,
  }, false, { signal }),
  errorMessage: '节点流量趋势加载失败。',
})
const { data: summary, loading: summaryLoading, error: summaryError, loaded: summaryLoaded } = summaryResource
const { data: observability, loading: observabilityLoading, error: observabilityError, loaded: observabilityLoaded } = observabilityResource
const { data: nodeSeries, loading: nodeSeriesLoading, error: nodeSeriesError, loaded: nodeSeriesLoaded } = nodeSeriesResource

const bucketOptions = [
  { label: '按天', value: 'day' },
  { label: '按小时', value: 'hour' },
  { label: '按分钟', value: 'minute' },
]
const bucketLabel = computed(() => bucket.value === 'minute' ? '分钟' : bucket.value === 'day' ? '天' : '小时')

const recordTable = useTrafficUsageTable(() => ({
  bucket: bucket.value, subscriptionId: subscriptionFilter.value || undefined,
  cursor: cursor.value || undefined, from: from.value, to: to.value, limit: limit.value,
}))
const {
  items: records,
  total,
  aggregates,
  facets: recordReferences,
  nextCursor,
  previousCursor,
  loading: recordLoading,
  refreshing,
  error: recordError,
  load: loadRecords,
  hasLoaded: recordsLoaded,
} = recordTable

const { statistics: recordStatistics, statisticsReady } = recordTable
const { loading: statisticsLoading, error: statisticsError, data: statisticsData } = recordStatistics
const loading = computed(() => statisticsLoading.value || recordLoading.value || summaryLoading.value || observabilityLoading.value || nodeSeriesLoading.value)
const subscriptionReferences = computed<Record<string, EntityReferenceData>>(() => recordReferences.value.subscriptions || {})
const nodeReferences = computed<Record<string, EntityReferenceData>>(() => recordReferences.value.nodes || {})

function subscriptionReference(id: number) {
  return subscriptionReferences.value[String(id)] || null
}

function nodeReference(id: number) {
  return nodeReferences.value[String(id)] || null
}

function formatMultiplier(value: number) {
  return `${(Number(value || 1000) / 1000).toLocaleString('zh-CN', { maximumFractionDigits: 3 })}×`
}

function normalizeBucket(value: unknown): TrafficUsageBucket {
  return value === 'minute' || value === 'day' ? value : 'hour'
}

function chartBucket(): TrafficUsageBucket {
  return trafficNodeBucket(bucket.value, from.value, to.value)
}

const trendKey = () => JSON.stringify([subscriptionFilter.value, from.value, to.value])
const loadRecordQuery = keyedLoad(() => JSON.stringify([trendKey(), bucket.value, cursor.value, limit.value]), recordTable)
const loadTrendQuery = keyedLoad(trendKey, observabilityResource)
const loadNodeQuery = keyedLoad(() => JSON.stringify([trendKey(), chartBucket()]), nodeSeriesResource)

async function loadChanged(force = false) {
  await Promise.all([loadRecordQuery(force), recordTable.loadStatistics(force), loadTrendQuery(force), loadNodeQuery(force)])
}
async function loadAll() {
  await Promise.all([summaryResource.load(), loadChanged(true)])
}

async function syncURL(replace = false) {
  const location = {
    query: {
      ...(subscriptionFilter.value ? { subscription_id: subscriptionFilter.value } : {}),
      ...(bucket.value !== 'hour' ? { bucket: bucket.value } : {}),
      from: from.value,
      to: to.value,
      ...(cursor.value ? { cursor: cursor.value } : {}),
      ...(limit.value !== 25 ? { limit: String(limit.value) } : {}),
    },
  }
  await (replace ? router.replace(location) : router.push(location))
}

function normalizeRange() {
  const range = resolveHistoryRange({ from: from.value, to: to.value }, 7)
  from.value = range.from
  to.value = range.to
}

async function applyFilters() {
  normalizeRange()
  cursor.value = ''
  await syncURL()
  await loadChanged()
}

async function clearFilters() {
  subscriptionFilter.value = ''
  bucket.value = 'hour'
  cursor.value = ''
  await syncURL()
  await loadChanged()
}

async function changeCursor(value: string | null) {
  if (!value) return
  cursor.value = value
  await syncURL()
  await loadChanged()
}

async function changeLimit(value: number) {
  limit.value = allowedPageSizes.includes(value) ? value : 25
  cursor.value = ''
  await syncURL()
  await loadChanged()
}

watch(() => route.fullPath, async () => {
  const nextSubscription = String(route.query.subscription_id || '')
  const nextBucket: TrafficUsageBucket = normalizeBucket(route.query.bucket)
  const nextCursorValue = String(route.query.cursor || '')
  const nextRange = resolveHistoryRange(route.query, 7)
  const rawLimit = Number(route.query.limit)
  const nextLimit = allowedPageSizes.includes(rawLimit) ? rawLimit : 25
  if (
    nextSubscription !== subscriptionFilter.value
    || nextBucket !== bucket.value
    || nextCursorValue !== cursor.value
    || nextRange.from !== from.value
    || nextRange.to !== to.value
    || nextLimit !== limit.value
  ) {
    subscriptionFilter.value = nextSubscription
    bucket.value = nextBucket
    cursor.value = nextCursorValue
    from.value = nextRange.from
    to.value = nextRange.to
    limit.value = nextLimit
    await loadChanged()
  }
})

onMounted(async () => {
  if (!route.query.from || !route.query.to) await syncURL(true)
  await loadAll()
})

</script>
