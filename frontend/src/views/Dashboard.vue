<template>
  <section class="standard-page">
    <PageHeader
      class="dashboard-header"
      title="运营工作台"
    >
      <template #actions>
        <div class="dashboard-controls">
          <UiSegmentedControl class="period-switch" label="经营指标周期" :options="periodOptions" :model-value="selectedRange" @update:model-value="changeRange($event as DashboardRange)" />
          <PageRefreshButton label="刷新运营工作台" variant="ghost" :loading="refreshing" @click="load" />
        </div>
      </template>
    </PageHeader>

    <section class="dashboard-overview-panel" aria-labelledby="dashboard-business-title">
    <div class="dashboard-group-heading business-heading">
      <div class="dashboard-group-copy"><h2 id="dashboard-business-title">经营概览</h2><p class="dashboard-period-copy"><span v-overflow-title="periodDescription">{{ overview ? periodDescription : '所选周期的经营统计' }}</span><span>查询时区 {{ overview?.period.timezone || '—' }}</span></p></div>
      <RemoteReadStatus compact label="经营统计" :phase="overviewResource.phase.value" :error="error" :last-success-at="overviewResource.lastSuccessAt.value" @retry="overviewResource.load()" />
    </div>

    <UiMetricStrip variant="plain" :columns="6" class="dashboard-section business-metrics" :aria-busy="!overview && loading || undefined">
      <MetricCard
        v-for="metric in businessMetrics"
        :key="metric.label"
        :label="metric.label"
        :loading="!overview && loading"
        :value="metric.value"
        :icon="metric.icon"
        :status="metric.label === '有效订阅' ? metric.status : undefined"
        tone="neutral"
        icon-tone="neutral"
        :meta="metric.meta"
      >
        <template #meta>
          <MetricComparison v-if="metric.comparison" :current="metric.comparison[0]" :previous="metric.comparison[1]" :label="comparisonLabel" compact :format="metric.label === '本期实收' ? comparisonMoney : metric.label === '本期流量' ? formatBytes : undefined" />
          <span v-else>{{ metric.meta }}</span>
        </template>
      </MetricCard>
    </UiMetricStrip>
    </section>

    <div class="section-grid dashboard-section">
      <UiSection variant="outlined" class="span-8" title="经营趋势" :description="trendDescription">
        <template #meta>
          <UiSegmentedControl label="趋势指标" :model-value="trendUsesRevenue ? 'revenue' : 'orders'" :options="trendMeasureOptions" @update:model-value="trendMeasure = $event as 'revenue' | 'orders'" />
          <UiButton as-child variant="ghost" size="sm"><RouterLink to="/admin/orders">
            订单明细<UiIcon name="chevron" />
          </RouterLink></UiButton>
        </template>

        <div class="dashboard-trend-status"><RemoteReadStatus compact label="经营趋势" :phase="trendResource.phase.value" :error="trendResource.error.value" :last-success-at="trendResource.lastSuccessAt.value" @retry="trendResource.load()" /></div>
        <div v-if="trend?.trend.length" class="trend-shell">
          <div class="trend-summary">
            <div>
              <span>本期实收</span>
              <strong>{{ trendRevenueValue }}</strong>
            </div>
            <div>
              <span>已支付订单</span>
              <strong>{{ formatNumber(trendTotals.paid_orders) }}</strong>
            </div>
            <div>
              <span>构成</span>
              <strong>{{ formatNumber(trendTotals.new_orders) }} 新购 · {{ formatNumber(trendTotals.renew_orders) }} 续费</strong>
            </div>
          </div>

          <div class="trend-chart" role="group" :aria-label="trendAriaLabel">
            <div class="trend-y-axis" aria-hidden="true"><span v-for="tick in trendTicks" :key="tick" :style="{ top: `${trendY(tick) / 2}%` }">{{ trendAxisLabel(tick) }}</span></div>
            <div class="trend-plot">
              <svg class="trend-line-chart" viewBox="0 0 1000 200" preserveAspectRatio="none" aria-hidden="true">
                <line v-for="tick in trendTicks" :key="tick" class="trend-grid-line" x1="0" x2="1000" :y1="trendY(tick)" :y2="trendY(tick)" vector-effect="non-scaling-stroke" />
                <polygon class="trend-area" :points="trendAreaPoints" />
                <polyline class="trend-line" :points="trendLinePoints" vector-effect="non-scaling-stroke" />
                <line v-if="activeTrendPoint" class="trend-crosshair" :x1="activeTrendPoint.x" :x2="activeTrendPoint.x" y1="0" y2="192" vector-effect="non-scaling-stroke" />
              </svg>
              <div class="trend-points" :style="{ gridTemplateColumns: `repeat(${trend.trend.length}, minmax(0, 1fr))` }">
                <Tooltip v-for="(point, index) in trend.trend" :key="point.bucket_start" :text="trendPointTitle(point)" :delay-duration="0" :open="activeTrendIndex === index" @update:open="value => { if (!value && activeTrendIndex === index) activeTrendIndex = null }">
                  <div class="trend-point" role="img" tabindex="0" :aria-label="trendPointTitle(point)" @pointermove="event => { if (event.pointerType !== 'touch') activeTrendIndex = index }" @pointerleave="activeTrendIndex = null" @focus="activeTrendIndex = index" @blur="activeTrendIndex = null" @click="activeTrendIndex = index">
                    <span class="trend-dot" :class="{ active: activeTrendIndex === index || trend.trend.length === 1 }" :style="{ top: `${trendY(trendValue(point)) / 2}%` }" />
                  </div>
                  <template #content>
                    <div class="trend-point-detail">
                      <time :datetime="point.bucket_start">{{ formatDashboardTime(point.bucket_start, trend.period.timezone) }}</time>
                      <strong>{{ trendUsesRevenue ? trendPointRevenue(point) : `已支付订单 ${formatNumber(point.paid_orders)} 笔` }}</strong>
                      <span>{{ trendUsesRevenue ? `已支付订单 ${formatNumber(point.paid_orders)} 笔` : trendPointRevenue(point) }}</span>
                      <span>新购 {{ formatNumber(point.new_orders) }} · 续费 {{ formatNumber(point.renew_orders) }}</span>
                    </div>
                  </template>
                </Tooltip>
              </div>
            </div>
            <div class="trend-x-axis" aria-hidden="true" :style="{ gridTemplateColumns: `repeat(${trend.trend.length}, minmax(0, 1fr))` }"><span v-for="(point, index) in trend.trend" :key="point.bucket_start">{{ showTrendLabel(index) ? trendBucketLabel(point.bucket_start) : '' }}</span></div>
          </div>

          <div class="trend-legend">{{ trendUsesRevenue ? trend.currency : '订单量' }}</div>
        </div>
        <div v-else class="trend-placeholder">
          <EmptyState v-if="trend" icon="billing" title="当前周期暂无已支付订单" description="产生已支付订单后显示所选周期的经营趋势。" />
          <div v-else-if="trendResource.loading.value" class="trend-loading" role="status"><Skeleton class="ui-skeleton" v-for="index in 7" :key="index" /><span class="sr-only">正在加载经营趋势</span></div>
          <p v-else class="dashboard-section-placeholder">经营趋势暂不可用，请查看上方失败原因。</p>
        </div>
      </UiSection>

      <UiSection variant="outlined"
        class="span-4"
        title="当前待办与异常"

      >
        <div v-if="!current" class="dashboard-section-placeholder" role="status">{{ currentResource.error.value ? '当前待办暂不可用' : '正在读取当前待办…' }}</div>
        <div v-else-if="actionQueue.length" class="attention-list">
          <RouterLink v-for="item in actionQueue" :key="item.key" :to="item.to" :class="item.tone">
            <span class="queue-icon"><UiIcon :name="item.icon" /></span>
            <div class="queue-copy">
              <span class="queue-label">{{ item.label }}</span>
              <strong>{{ formatNumber(item.value) }}</strong>
              <p>{{ item.description }}</p>
            </div>
            <UiIcon name="chevron" />
          </RouterLink>
        </div>
        <EmptyState
          v-else
          icon="tasks"
          title="当前没有需要处理的异常"
          description="历史失败仍可在任务、订单、发布和审计记录中追溯。"
        />
      </UiSection>
    </div>

    <section class="dashboard-overview-panel" aria-labelledby="dashboard-service-title">
    <div class="dashboard-group-heading">
      <div class="dashboard-group-copy"><h2 id="dashboard-service-title">服务概况</h2></div>
      <RemoteReadStatus compact label="当前服务状态" :phase="currentResource.phase.value" :error="currentResource.error.value" :last-success-at="currentResource.lastSuccessAt.value" @retry="currentResource.load()" />
    </div>
    <UiMetricStrip variant="plain" class="dashboard-section service-metrics" :aria-busy="!current && currentResource.loading.value || undefined">
      <template v-if="current">
      <MetricCard label="有效订阅" :value="formatNumber(current.active_subscriptions)" icon="plans" :meta="current.subscriptions.expiring_within_3d ? `${formatNumber(current.subscriptions.expiring_within_3d)} 个将在 3 天内到期` : ''" />
      <MetricCard
        label="已观测活跃订阅"
        :value="observedValue(observedSubscriptions)"
        icon="plans"
        :status="observedSubscriptions === null ? '未采集' : '覆盖未知'"
        tone="neutral"
        icon-tone="neutral"
        :title="observedSubscriptions === null ? '尚未采集连接数据' : '覆盖范围及更新时间待确认'"
      />
      <MetricCard
        label="已观测连接"
        :value="observedValue(observedFlows)"
        icon="nodes"
        :status="observedFlows === null ? '未采集' : '覆盖未知'"
        tone="neutral"
        icon-tone="neutral"
        :title="observedFlows === null ? '尚未采集连接数据' : '覆盖范围及更新时间待确认'"
      />

      <MetricCard
        label="在线节点"
        :value="`${formatNumber(current.service.online_nodes)} / ${formatNumber(current.service.enabled_nodes)}`"
        icon="nodes"

        tone="neutral"
        icon-tone="neutral"
        title="依据最近两分钟心跳"
      />
      </template>
      <template v-else><MetricCard v-for="metric in serviceMetricLabels" :key="metric.label" :label="metric.label" value="—" :icon="metric.icon" :loading="currentResource.loading.value" meta="尚未取得服务数据" /></template>
    </UiMetricStrip>
    </section>

    <div class="section-grid dashboard-section dashboard-health">
      <UiSection variant="outlined" class="span-6" title="订阅健康">
        <div v-if="!current" class="health-placeholder" aria-label="正在加载订阅健康" role="status"><template v-if="currentResource.loading.value"><Skeleton class="ui-skeleton" v-for="index in 4" :key="index" /></template><p v-else>订阅健康暂不可用，请查看当前服务状态的失败原因。</p></div>
        <div v-else class="health-list">
          <RouterLink v-for="item in subscriptionHealth" :key="item.label" to="/admin/subscriptions" class="health-row">
            <span class="health-copy">
              <strong>{{ item.label }}</strong>
              <small class="sr-only">{{ item.description }}</small>
            </span>
            <span class="health-value">{{ formatNumber(item.value) }}</span>
            <StatusBadge :tone="item.tone">{{ item.status }}</StatusBadge>
            <UiIcon name="chevron" />
          </RouterLink>
        </div>
      </UiSection>

      <UiSection variant="outlined" class="span-6" title="服务基础设施">
        <div v-if="!current" class="health-placeholder" aria-label="正在加载服务基础设施" role="status"><template v-if="currentResource.loading.value"><Skeleton class="ui-skeleton" v-for="index in 5" :key="index" /></template><p v-else>服务基础设施暂不可用，请查看当前服务状态的失败原因。</p></div>
        <div v-else class="health-list">
          <RouterLink v-for="item in infrastructure" :key="item.label" :to="item.to" class="health-row">
            <span class="health-copy">
              <strong>{{ item.label }}</strong>
              <small class="sr-only">{{ item.description }}</small>
            </span>
            <span class="health-value">{{ item.value }}</span>
            <StatusBadge :tone="item.tone">{{ item.status }}</StatusBadge>
            <UiIcon name="chevron" />
          </RouterLink>
        </div>
      </UiSection>
    </div>

    <UiSection variant="outlined"
      v-if="current && !deliveryReady"
      class="dashboard-section"
      title="交付准备尚未完成"
      description="只在基础交付能力未就绪时出现；全部完成后自动退出正常运营首屏。"
    >
      <template #meta><strong class="progress-count">{{ completedReadiness }}/{{ readiness.length }}</strong></template>
      <div class="readiness-list">
        <RouterLink
          v-for="(item, index) in readiness"
          :key="item.label"
          :to="item.to"
          :class="{ complete: item.complete }"
        >
          <span class="step-index">
            <UiIcon v-if="item.complete" name="check" />
            <template v-else>{{ index + 1 }}</template>
          </span>
          <div>
            <strong>{{ item.label }}</strong>
            <p>{{ item.description }}</p>
          </div>
          <StatusBadge :tone="item.complete ? 'success' : 'warning'">
            {{ item.complete ? '已就绪' : '待配置' }}
          </StatusBadge>
          <UiIcon name="chevron" />
        </RouterLink>
      </div>
    </UiSection>

    <div class="section-grid dashboard-section">
      <DashboardTrafficRanking class="span-6" title="节点流量排行" :items="nodeRankings?.nodes || []" :range="nodeRange" :comparison-label="rangeComparison(nodeRange)" :loading="nodesLoading" :error="nodesError" :as-of="nodeRankings?.as_of" @update:range="changeNodeRange" @refresh="nodesResource.load()" />
      <DashboardTrafficRanking class="span-6" title="用户流量排行" :items="userRankings?.users || []" :range="userRange" :comparison-label="rangeComparison(userRange)" :loading="usersLoading" :error="usersError" :as-of="userRankings?.as_of" @update:range="changeUserRange" @refresh="usersResource.load()" />
    </div>
    <UiSection variant="outlined" class="dashboard-section" title="最近重要运营事件" description="最近的配置发布与关键运营记录。">
      <TableSkeleton v-if="deploymentsLoading && !deployments.length" label="正在加载最近事件" :columns="4" :rows="4" />
      <template #actions>
        <RemoteReadStatus compact label="最近事件" :phase="eventsResource.phase.value" :error="deploymentsError" :last-success-at="eventsResource.lastSuccessAt.value" @retry="eventsResource.load()" />
        <UiButton as-child variant="ghost" size="sm"><RouterLink to="/admin/protocols">
          协议服务<UiIcon name="chevron" />
        </RouterLink></UiButton>
      </template>

      <DataTable
        v-if="deployments.length"
        caption="最近协议配置发布结果"
        :row-count="deployments.length"
        :min-width="620"
        table-class="dashboard-deployment-table"
      >
        <thead>
          <tr>
            <th class="table-primary-column">事件</th>
            <th>节点</th>
            <th>结果</th>
            <th data-column-priority="2">时间</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="deployment in deployments" :key="deployment.id">
            <td class="table-primary-column"><strong>协议端点 #{{ deployment.protocol_endpoint_id }} 配置发布</strong></td>
            <td class="numeric-column">#{{ deployment.node_id }}</td>
            <td>
              <StatusBadge :tone="deploymentTone(deployment.status)">{{ deploymentLabel(deployment.status) }}</StatusBadge>
            </td>
            <td data-column-priority="2"><TimeBadge :value="deployment.created_at" /></td>
          </tr>
        </tbody>
      </DataTable>
      <EmptyState v-else-if="!deploymentsLoading && !deploymentsError" icon="nodes" title="暂无重要运营事件" description="发生配置发布等高价值运营事件后会显示在这里。" />
    </UiSection>
  </section>
</template>

<script setup lang="ts">
import { vOverflowTitle } from '../directives/overflowTitle'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { fetchProtocolDeployments, type ProtocolDeployment } from '../api/client'
import {
  fetchDashboardCurrent, fetchDashboardStatistics, fetchDashboardTrend,
  fetchDashboardTrafficRankings,
  type DashboardTrafficRankings,
  type DashboardCurrent, type DashboardStatistics, type DashboardTrend,
  type DashboardRange,
  type DashboardTrendPoint,
} from '../api/dashboard'
import RemoteReadStatus from '../components/RemoteReadStatus.vue'
import DataTable from '../components/DataTable.vue'
import EmptyState from '../components/EmptyState.vue'
import MetricCard from '../components/MetricCard.vue'
import MetricComparison from '../components/MetricComparison.vue'
import DashboardTrafficRanking from '../components/DashboardTrafficRanking.vue'
import PageHeader from '../components/PageHeader.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TimeBadge from '../components/TimeBadge.vue'
import TableSkeleton from '../components/TableSkeleton.vue'
import UiIcon from '../components/UiIcon.vue'
import UiButton from '../components/UiButton.vue'
import UiSegmentedControl from '../components/UiSegmentedControl.vue'
import { Skeleton } from '../components/ui/skeleton'
import Tooltip from '../components/ui/Tooltip.vue'
import { formatBytes, formatCurrency, formatNumber, formatUnknownValue } from '../utils/format'
import { buildDashboardAttention } from '../utils/dashboardHealth'
import { useRemoteResource } from '../composables/useRemoteResource'

const selectedRange = ref<DashboardRange>('7d')
const nodeRange = ref<DashboardRange>('today')
const userRange = ref<DashboardRange>('today')
const currentResource = useRemoteResource<DashboardCurrent | null>({ initial: () => null, fetch: ({ signal }) => fetchDashboardCurrent({ signal }), errorMessage: '当前服务状态加载失败。', timeoutMs: 5_000 })
const overviewResource = useRemoteResource<DashboardStatistics | null>({ initial: () => null, fetch: ({ signal }) => fetchDashboardStatistics(selectedRange.value, { signal }), errorMessage: '经营统计加载失败。', timeoutMs: 10_000 })
const trendResource = useRemoteResource<DashboardTrend | null>({ initial: () => null, fetch: ({ signal }) => fetchDashboardTrend(selectedRange.value, { signal }), errorMessage: '经营趋势加载失败。', timeoutMs: 10_000 })
const { data: current } = currentResource
const { data: trend } = trendResource
const nodesResource = useRemoteResource<DashboardTrafficRankings | null>({ initial: () => null, fetch: ({ signal }) => fetchDashboardTrafficRankings(nodeRange.value, { signal, dimension: 'nodes' }), errorMessage: '节点排行加载失败。' })
const usersResource = useRemoteResource<DashboardTrafficRankings | null>({ initial: () => null, fetch: ({ signal }) => fetchDashboardTrafficRankings(userRange.value, { signal, dimension: 'users' }), errorMessage: '用户排行加载失败。' })
const eventsResource = useRemoteResource<ProtocolDeployment[]>({ initial: () => [], fetch: async ({ signal }) => (await fetchProtocolDeployments({ limit: 8 }, { signal })).items || [], errorMessage: '最近事件加载失败。' })
const { data: overview, loading, error } = overviewResource
const { data: nodeRankings, loading: nodesLoading, error: nodesError } = nodesResource
const { data: userRankings, loading: usersLoading, error: usersError } = usersResource
const { data: deployments, loading: deploymentsLoading, error: deploymentsError } = eventsResource
const refreshing = computed(() => currentResource.loading.value || trendResource.loading.value || loading.value || nodesLoading.value || usersLoading.value || deploymentsLoading.value)
function rangeComparison(range: DashboardRange) { return { today: '对比昨日同期', '7d': '对比前 7 天同期', '30d': '对比前 30 天同期', month: '对比上月同期' }[range] }
const comparisonLabel = computed(() => rangeComparison(selectedRange.value))
const trendMeasure = ref<'revenue' | 'orders'>('revenue')
const trendMeasureOptions = computed(() => trend.value?.mixed_currency ? [{ label: '订单量', value: 'orders' }] : [{ label: '实收金额', value: 'revenue' }, { label: '订单量', value: 'orders' }])
const comparisonMoney = (value: number) => formatCurrency(value, overview.value?.business.currency || 'CNY')
const dashboardRefreshIntervalMS = 3_000
let slowRefreshAt = Date.now()
let dashboardRefreshTimer: number | undefined

const periodOptions: Array<{ label: string; value: DashboardRange }> = [
  { label: '今天', value: 'today' },
  { label: '本月', value: 'month' },
  { label: '近 7 天', value: '7d' },
  { label: '近 30 天', value: '30d' },
]

const revenueValue = computed(() => {
  if (!overview.value) return '—'
  if (overview.value.business.mixed_currency) return '多币种'
  return formatCurrency(overview.value.business.revenue_cents, overview.value.business.currency || 'CNY')
})

const serviceMetricLabels = [{ label: '有效订阅', icon: 'plans' }, { label: '已观测活跃订阅', icon: 'plans' }, { label: '已观测连接', icon: 'nodes' }, { label: '在线节点', icon: 'nodes' }]
const businessMetrics = computed(() => {
  if (!overview.value) return [{ label: '本期实收', icon: 'dollar' }, { label: '已支付订单', icon: 'billing' }, { label: '新增订阅', icon: 'plans' }, { label: '续费成功', icon: 'refresh' }, { label: '新增用户', icon: 'users' }, { label: '本期流量', icon: 'traffic' }].map(metric => ({ ...metric, value: '—', status: '', meta: '尚未取得经营统计', comparison: undefined }))
  const business = overview.value.business
  return [
    {
      label: '本期实收',
      comparison: business.mixed_currency ? undefined : [business.revenue_cents, business.previous_revenue_cents],
      value: revenueValue.value,
      icon: 'dollar',
      status: periodStatus.value,
      meta: business.mixed_currency
        ? '当前/上一周期包含多币种，金额不做跨币种比较'
        : comparisonText(business.revenue_cents, business.previous_revenue_cents, true),
    },
    {
      label: '已支付订单',
      comparison: [business.paid_orders, business.previous_paid_orders],
      value: formatNumber(business.paid_orders),
      icon: 'billing',
      status: periodStatus.value,
      meta: `${formatNumber(business.new_orders)} 新购 · ${formatNumber(business.renew_orders)} 续费 · ${comparisonText(business.paid_orders, business.previous_paid_orders)}`,
    },
    {
      label: '新增订阅',
      comparison: [business.new_subscriptions, business.previous_new_subscriptions],
      value: formatNumber(business.new_subscriptions),
      icon: 'plans',
      status: periodStatus.value,
      meta: comparisonText(business.new_subscriptions, business.previous_new_subscriptions),
    },
    {
      label: '续费成功',
      comparison: [business.renew_orders, business.previous_renew_orders ?? 0],
      value: formatNumber(business.renew_orders),
      icon: 'refresh',
      status: periodStatus.value,
      meta: '',
    },
    { label: '新增用户', value: formatNumber(business.new_users ?? 0), icon: 'users', status: periodStatus.value, comparison: [business.new_users ?? 0, business.previous_new_users ?? 0], meta: '' },
    { label: '本期流量', value: formatBytes(overview.value.traffic.traffic_bytes), icon: 'traffic', status: periodStatus.value, comparison: [overview.value.traffic.traffic_bytes, overview.value.traffic.previous_traffic_bytes ?? 0], meta: '按计费流量口径聚合' },

  ]
})

const periodStatus = computed(() => ({ today: '今天', '7d': '近 7 天', '30d': '近 30 天', month: '本月' }[selectedRange.value]))
const periodDescription = computed(() => overview.value ? `${formatDashboardTime(overview.value.period.from)} – ${formatDashboardTime(overview.value.period.to)}` : '')

const actionQueue = computed(() => {
  if (!current.value) return []
  return buildDashboardAttention({
    nodes_offline: current.value.attention.nodes_offline,
    deployments_failed: current.value.attention.deployments_unresolved,
    tickets_pending: current.value.attention.tickets_pending,
  })
})

const subscriptionHealth = computed(() => {
  if (!current.value) return []
  const health = current.value.subscriptions
  return [
    { label: '24 小时内到期', value: health.expiring_within_24h, status: health.expiring_within_24h > 0 ? '关注' : '正常', tone: health.expiring_within_24h > 0 ? 'warning' as const : 'success' as const, description: '仍有效且将在未来 24 小时内到期' },
    { label: '3 天内到期', value: health.expiring_within_3d, status: health.expiring_within_3d > 0 ? '关注' : '正常', tone: health.expiring_within_3d > 0 ? 'warning' as const : 'success' as const, description: '累计口径，包含 24 小时内到期订阅' },
    { label: '7 天内到期', value: health.expiring_within_7d, status: health.expiring_within_7d > 0 ? '观察' : '正常', tone: health.expiring_within_7d > 0 ? 'info' as const : 'success' as const, description: '用于提前观察未来一周订阅生命周期' },
    { label: '流量已耗尽', value: health.quota_exhausted, status: health.quota_exhausted > 0 ? '流量用尽' : '正常', tone: health.quota_exhausted > 0 ? 'warning' as const : 'success' as const, description: '当前周期配额已用尽，订阅记录仍可管理' },
  ]
})

const infrastructure = computed(() => {
  if (!current.value) return []
  const infra = current.value.infrastructure
  const offline = current.value.attention.nodes_offline
  return [
    { label: '节点运行', value: `${formatNumber(infra.connector_online)} / ${formatNumber(infra.nodes_enabled)}`, status: offline > 0 ? '需处理' : '正常', tone: offline > 0 ? 'danger' as const : 'success' as const, description: 'Connector 当前在线 / 已启用节点', to: '/admin/nodes' },
    { label: 'SSH 运维通道', value: `${formatNumber(infra.ssh_verified)} 已验证`, status: infra.nodes_total > 0 && infra.ssh_verified < infra.nodes_total ? '未完整' : '已就绪', tone: infra.nodes_total > 0 && infra.ssh_verified < infra.nodes_total ? 'warning' as const : 'success' as const, description: '已验证凭证并固定主机身份的节点', to: '/admin/nodes' },
    { label: '流量上报凭证', value: `${formatNumber(infra.traffic_ready)} 已配置`, status: infra.nodes_total > 0 && infra.traffic_ready < infra.nodes_total ? '未完整' : '已就绪', tone: infra.nodes_total > 0 && infra.traffic_ready < infra.nodes_total ? 'warning' as const : 'success' as const, description: '已配置可信流量上报的节点', to: '/admin/nodes' },
    { label: '协议交付', value: `${formatNumber(infra.active_protocol_endpoints)} / ${formatNumber(infra.protocol_endpoints)}`, status: infra.active_protocol_endpoints > 0 ? '可用' : '未就绪', tone: infra.active_protocol_endpoints > 0 ? 'success' as const : 'warning' as const, description: '已启用协议端点 / 全部协议端点', to: '/admin/protocols' },
    { label: '配置发布', value: infra.unresolved_deployments ? `${formatNumber(infra.unresolved_deployments)} 未恢复` : '已收敛', status: infra.unresolved_deployments ? '需处理' : '正常', tone: infra.unresolved_deployments ? 'danger' as const : 'success' as const, description: '只统计每个协议端点最新一次发布仍然失败的状态', to: '/admin/protocols?deployment=failed' },
  ]
})

const readiness = computed(() => {
  if (!current.value) return []
  const infra = current.value.infrastructure
  return [
    { label: '登记运行主机', description: `${formatNumber(infra.nodes_total)} 台主机已纳入平台`, complete: infra.nodes_total > 0, to: '/admin/nodes' },
    { label: '建立 Zero 主动连接', description: `${formatNumber(infra.connector_online)} 台在两分钟窗口内有 Connector 心跳`, complete: infra.connector_online > 0, to: '/admin/nodes' },
    { label: '验证 SSH 运维通道', description: `${formatNumber(infra.ssh_verified)} 台已验证 SSH 主机身份`, complete: infra.ssh_verified > 0, to: '/admin/nodes' },
    { label: '配置流量上报凭证', description: `${formatNumber(infra.traffic_ready)} 台具备可信上报凭证`, complete: infra.traffic_ready > 0, to: '/admin/nodes' },
    { label: '登记协议交付资源', description: `${formatNumber(infra.active_protocol_endpoints)} 个已启用，共 ${formatNumber(infra.protocol_endpoints)} 个端点`, complete: infra.active_protocol_endpoints > 0, to: '/admin/protocols' },
    { label: '建立销售与交付链路', description: `${formatNumber(infra.published_plans)} 个已发布商品`, complete: infra.published_plans > 0, to: '/admin/plans' },
  ]
})

const completedReadiness = computed(() => readiness.value.filter(item => item.complete).length)
const deliveryReady = computed(() => readiness.value.length > 0 && readiness.value.every(item => item.complete))
const trendUsesRevenue = computed(() => Boolean(trend.value && !trend.value.mixed_currency && trendMeasure.value === 'revenue'))
const trendTotals = computed(() => (trend.value?.trend || []).reduce((sum, point) => ({ revenue_cents: sum.revenue_cents + point.revenue_cents, paid_orders: sum.paid_orders + point.paid_orders, new_orders: sum.new_orders + point.new_orders, renew_orders: sum.renew_orders + point.renew_orders }), { revenue_cents: 0, paid_orders: 0, new_orders: 0, renew_orders: 0 }))
const trendRevenueValue = computed(() => trend.value?.mixed_currency ? '多币种' : formatCurrency(trendTotals.value.revenue_cents, trend.value?.currency || 'CNY'))
const activeTrendIndex = ref<number | null>(null)
function trendValue(point: DashboardTrendPoint) {
  const value = trendUsesRevenue.value ? point.revenue_cents : point.paid_orders
  return Number.isFinite(value) ? Math.max(0, value) : 0
}
const trendScale = computed(() => {
  const max = Math.max(1, ...(trend.value?.trend || []).map(trendValue))
  const rawStep = max / 3, magnitude = 10 ** Math.floor(Math.log10(rawStep))
  const step = Math.max(1, [1, 2, 5, 10].find(value => value * magnitude >= rawStep)! * magnitude)
  return { step, max: Math.ceil(max / step) * step }
})
const trendTicks = computed(() => Array.from({ length: Math.round(trendScale.value.max / trendScale.value.step) + 1 }, (_, index) => index * trendScale.value.step))
function trendY(value: number) { return 192 - value / trendScale.value.max * 184 }
const trendCoordinates = computed(() => (trend.value?.trend || []).map((point, index, points) => ({ x: (index + 0.5) / points.length * 1000, y: trendY(trendValue(point)) })))
const trendLinePoints = computed(() => trendCoordinates.value.map(point => `${point.x},${point.y}`).join(' '))
const trendAreaPoints = computed(() => {
  const points = trendCoordinates.value
  return points.length > 1 ? `${points[0]!.x},192 ${trendLinePoints.value} ${points[points.length - 1]!.x},192` : ''
})
const activeTrendPoint = computed(() => activeTrendIndex.value === null ? null : trendCoordinates.value[activeTrendIndex.value])
function trendAxisLabel(value: number) { return trendUsesRevenue.value ? formatCurrency(value, trend.value?.currency || 'CNY') : formatCompactCount(value) }
function showTrendLabel(index: number) { const count = trend.value?.trend.length || 0; return index === 0 || index === count - 1 || index % Math.ceil(count / 6) === 0 }
const trendDescription = computed(() => trend.value?.mixed_currency
  ? '当前包含多币种，趋势按订单量展示。'
  : trendUsesRevenue.value ? '查看所选周期的实收变化；可切换为订单量。' : '查看所选周期的已支付订单量。')
const trendAriaLabel = computed(() => trend.value ? `${periodStatus.value} 经营趋势，共 ${(trend.value?.trend || []).length} 个时间桶` : '经营趋势')

function comparisonText(current: number, previous: number, money = false) {
  if (!Number.isFinite(current) || !Number.isFinite(previous)) return '暂无可比数据'
  if (previous === 0) {
    if (current === 0) return '与上期持平'
    const delta = money && overview.value && !overview.value.business.mixed_currency
      ? formatCurrency(current, overview.value.business.currency || 'CNY')
      : formatNumber(current)
    return `上期为 0 · 本期 +${delta}`
  }
  const percent = ((current - previous) / Math.abs(previous)) * 100
  return `较上期 ${percent > 0 ? '+' : ''}${percent.toFixed(1)}%`
}

// Old servers expose these snapshots in active_* fields. Treat them as
// observations too; an old boolean alone never proves full runtime coverage.
function snapshotCount(field: 'active_subscriptions' | 'active_flows'): number | null {
  const service = current.value?.service
  if (!service) return null
  const observed = field === 'active_flows' ? 'observed_active_flows' : 'observed_active_subscriptions'
  const value = Object.prototype.hasOwnProperty.call(service, observed) ? service[observed] : service[field]
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 ? value : null
}
const observedSubscriptions = computed(() => snapshotCount('active_subscriptions'))
const observedFlows = computed(() => snapshotCount('active_flows'))

function observedValue(value: number | null) {
  return value === null ? '未采集' : formatNumber(value)
}

function trendPointRevenue(point: DashboardTrendPoint) {
  return trend.value?.mixed_currency
    ? '多币种金额不合并展示'
    : `实收金额 ${formatCurrency(point.revenue_cents, trend.value?.currency || 'CNY')}`
}
function trendPointTitle(point: DashboardTrendPoint) {
  const revenue = trendPointRevenue(point), orders = `已支付订单 ${formatNumber(point.paid_orders)} 笔`
  return `${formatDashboardTime(point.bucket_start, trend.value?.period.timezone)} · ${trendUsesRevenue.value ? `${revenue} · ${orders}` : `${orders} · ${revenue}`}（新购 ${formatNumber(point.new_orders)} / 续费 ${formatNumber(point.renew_orders)}）`
}

function trendBucketLabel(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  if (trend.value?.period.bucket === 'hour') return new Intl.DateTimeFormat('zh-CN', { timeZone: trend.value?.period.timezone || 'UTC', hour: '2-digit', minute: '2-digit', hour12: false }).format(date)
  return new Intl.DateTimeFormat('zh-CN', { timeZone: trend.value?.period.timezone || 'UTC', month: '2-digit', day: '2-digit' }).format(date)
}

function formatCompactCount(value: number) {
  if (value >= 1000) return `${(value / 1000).toFixed(value >= 10000 ? 0 : 1)}k`
  return String(value)
}

function formatDashboardTime(value: string, timezone = overview.value?.period.timezone || trend.value?.period.timezone || 'UTC') {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return `${new Intl.DateTimeFormat('zh-CN', { timeZone: timezone, month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }).format(date)}`
}

function deploymentLabel(status: string) {
  return ({ running: '部署中', succeeded: '部署成功', failed: '部署失败' } as Record<string, string>)[status] || formatUnknownValue('状态', status)
}
function deploymentTone(status: string): 'info' | 'success' | 'danger' {
  return status === 'succeeded' ? 'success' : status === 'failed' ? 'danger' : 'info'
}

async function changeRange(range: DashboardRange) {
  selectedRange.value = range
  overviewResource.reset()
  trendResource.reset()
  await Promise.allSettled([overviewResource.load(), trendResource.load()])
}
async function changeNodeRange(range: DashboardRange) {
  nodeRange.value = range
  nodesResource.reset()
  await nodesResource.load()
}
async function changeUserRange(range: DashboardRange) {
  userRange.value = range
  usersResource.reset()
  await usersResource.load()
}
async function load() {
  await Promise.allSettled([currentResource.load(), overviewResource.load(), trendResource.load(), nodesResource.load(), usersResource.load(), eventsResource.load()])
}
onMounted(() => {
  void load()
  dashboardRefreshTimer = window.setInterval(() => {
    if (!document.hidden) {
      if (!currentResource.loading.value && currentResource.canAutoRefresh.value) void currentResource.load()
      if (!loading.value && overviewResource.canAutoRefresh.value) void overviewResource.load()
      if (Date.now() - slowRefreshAt < 15_000) return
      slowRefreshAt = Date.now()
      if (!trendResource.loading.value && trendResource.canAutoRefresh.value) void trendResource.load()
      if (!nodesLoading.value && nodesResource.canAutoRefresh.value) void nodesResource.load()
      if (!usersLoading.value && usersResource.canAutoRefresh.value) void usersResource.load()
      if (!deploymentsLoading.value && eventsResource.canAutoRefresh.value) void eventsResource.load()
    }
  }, dashboardRefreshIntervalMS)
})
onBeforeUnmount(() => {
  if (dashboardRefreshTimer !== undefined) window.clearInterval(dashboardRefreshTimer)
})
</script>

<style scoped>
.dashboard-overview-panel { min-width: 0; padding: 20px; border: 1px solid var(--border); border-radius: var(--radius-card); background: var(--card); }
.dashboard-overview-panel > .dashboard-group-heading { margin-bottom: 20px; }
@media (max-width: 600px) { .dashboard-overview-panel { padding: 16px; } }
.span-6 { grid-column: span 6; }
@media (max-width: 900px) { .span-6 { grid-column: 1 / -1; } }
.dashboard-section { margin-bottom: 18px; }
.dashboard-group-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; min-height: 48px; margin-block: 4px 8px; }
.dashboard-group-copy { min-width: 0; }
.dashboard-period-copy { display: grid; grid-template-rows: 18px 18px; }
.dashboard-period-copy span { overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
.health-placeholder { display: grid; gap: 16px; min-height: 280px; padding: 18px; }
.health-placeholder > :deep(.ui-skeleton) { height: 28px; }
.dashboard-health :deep(.health-list) { min-height: 280px; }
.dashboard-group-heading h2 { margin: 0; font-size: 16px; font-weight: 600; }
.dashboard-group-heading p { margin: 4px 0 0; color: var(--muted-foreground); font-size: 12px; }
.dashboard-controls { display: flex; align-items: center; justify-content: flex-end; gap: 10px; flex-wrap: wrap; }
.period-switch { flex: 0 0 auto; }
.period-context { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin: -4px 0 14px; color: var(--muted); font-size:12px; }
.business-metrics :deep(.metric-meta) { display: flex; flex-wrap: wrap; gap: 4px; align-items: baseline; min-height: 18px; }
.business-metrics :deep(.metric-card:first-child .overview-card-value) { font-size: 30px; }
.business-metrics :deep(.overview-card-description) { font-size: 12px; }
.service-metrics :deep(.overview-card-value) { font-size: 18px; }
.service-metrics :deep(.overview-card-label) { font-size: 12px; }
.service-metrics :deep(.overview-card-description) { font-size: 12px; }
.dashboard-trend-status { display: flex; justify-content: flex-end; height: 26px; }
.trend-placeholder { min-height: 365px; display: grid; align-items: center; }
.trend-loading { display: flex; height: 220px; gap: 12px; padding: 20px; align-items: stretch; }
.trend-loading > :deep(.ui-skeleton) { flex: 1; }
.dashboard-section-placeholder { min-height: 180px; padding: 24px 18px; color: var(--muted-foreground); font-size: 13px; }
.trend-shell { min-height: 365px; min-width: 0; padding: 8px 0 0; }
.trend-summary { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 16px; margin-bottom: 18px; padding-block: 2px 4px; }
.trend-summary > div { min-width: 0; padding: 3px 14px; }
.trend-summary > div:first-child { padding-left: 0; }
.trend-summary > div + div { border: 0; }
.trend-summary span { display: block; color: var(--muted); font-size: 11px; }
.trend-summary strong { display: block; margin-top: 5px; font-size: 15px; font-weight: 600; line-height: 1.3; overflow-wrap: anywhere; }
.trend-chart { display: grid; grid-template-columns: 64px minmax(0, 1fr); grid-template-rows: 220px 26px; gap: 8px 10px; }
.trend-y-axis { position: relative; color: var(--muted-foreground); font-size: 11px; font-variant-numeric: tabular-nums; }
.trend-y-axis > span { position: absolute; right: 0; transform: translateY(-50%); white-space: nowrap; }
.trend-plot { position: relative; min-width: 0; }
.trend-line-chart, .trend-points { position: absolute; inset: 0; width: 100%; height: 100%; }
.trend-grid-line { stroke: var(--line-subtle); stroke-width: 1; stroke-dasharray: 3 4; }
.trend-line { fill: none; stroke: var(--primary); stroke-width: 2; stroke-linejoin: round; stroke-linecap: round; }
.trend-area { fill: var(--primary); opacity: .05; }
.trend-crosshair { stroke: var(--muted-foreground); stroke-width: 1; stroke-dasharray: 3 3; opacity: .5; }
.trend-points { display: grid; }
.trend-point { position: relative; min-width: 0; cursor: crosshair; }
.trend-point:focus-visible { outline: 2px solid var(--ring); outline-offset: -2px; border-radius: 4px; }
.trend-dot { position: absolute; left: 50%; width: 8px; height: 8px; border: 2px solid var(--primary); border-radius: 50%; background: var(--card); opacity: 0; transform: translate(-50%, -50%); }
.trend-dot.active { opacity: 1; }
.trend-x-axis { display: grid; grid-column: 2; align-items: center; color: var(--muted-foreground); font-size: 11px; text-align: center; }
.trend-x-axis > span { white-space: nowrap; }
.trend-point-detail { display: grid; gap: 4px; min-width: 170px; }
.trend-point-detail time { color: var(--muted-foreground); font-size: 12px; }
.trend-point-detail strong { font-size: 14px; font-weight: 600; }
.trend-legend { display: flex; align-items: center; gap: 6px; margin-top: 4px; color: var(--muted-foreground); font-size: 11px; }
.trend-legend::before { width: 16px; height: 2px; background: var(--primary); content: ''; }
.attention-list, .health-list, .readiness-list { display: grid; padding-inline: 18px; }
.attention-list > a { min-width: 0; display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 12px; padding: 16px 4px; text-decoration: none; }
.attention-list > a + a, .health-row + .health-row, .readiness-list > a + a { border-top: 1px solid var(--line); }
.queue-icon { width: 36px; height: 36px; display: grid; place-items: center; border-radius: 8px; color: var(--warning); background: var(--warning-soft); }
.attention-list > a.danger .queue-icon { color: var(--danger); background: var(--danger-soft); }
.queue-copy { min-width: 0; }
.queue-label { display: block; font-size: 12px; font-weight: 600; }
.queue-copy strong { display: block; margin-top: 2px; font-size: 22px; line-height: 1.1; }
.queue-copy p { margin: 4px 0 0; color: var(--muted); font-size: 11px; }
.attention-list > a > .ui-icon { color: var(--subtle); }
.health-row { display: grid; grid-template-columns: minmax(0, 1fr) auto auto auto; align-items: center; gap: 10px; padding: 13px 4px; text-decoration: none; }
.health-copy { min-width: 0; }
.health-copy strong { display: block; font-size: 12px; font-weight: 600; }
.health-copy small { display: block; margin-top: 3px; color: var(--muted); font-size: 11px; line-height: 1.4; }
.health-value { font-size: 13px; font-weight: 600; white-space: nowrap; }
.health-row > .ui-icon { color: var(--subtle); }
.progress-count { font-size: 11px; }
.readiness-list > a { display: grid; grid-template-columns: auto minmax(0, 1fr) auto auto; gap: 12px; align-items: center; padding: 13px 4px; text-decoration: none; }
.readiness-list strong { display: block; font-size: 12px; font-weight: 600; }
.readiness-list p { margin: 3px 0 0; color: var(--muted); font-size: 11px; }
.step-index { width: 27px; height: 27px; display: grid; place-items: center; border-radius: 50%; background: var(--warning-soft); color: var(--warning); font-size:12px; font-weight:600; }
.readiness-list > a.complete .step-index { background: var(--success-soft); color: var(--success); }
@media (max-width: 900px) {
  .dashboard-controls { justify-content: flex-start; }
  .period-switch { max-width: 100%; }
  .trend-summary { gap: 12px; }
  .trend-summary > div, .trend-summary > div:first-child { padding: 0; }
  .trend-summary > div + div { border-left: 0; }
}
@media (max-width: 560px) {
  .dashboard-group-heading { align-items: flex-start; }
  .dashboard-group-heading p { margin-top: 3px; }
  .trend-shell { padding-inline: 0; }
}
@media (max-width: 640px) {
  .health-row { grid-template-columns: minmax(0, 1fr) auto auto; }
  .health-row > .ui-icon { display: none; }
  .readiness-list > a { grid-template-columns: auto minmax(0, 1fr) auto; }
  .readiness-list > a > .ui-icon { display: none; }
  .trend-chart { grid-template-columns: 56px minmax(0, 1fr); grid-template-rows: 190px 26px; gap: 8px 6px; }
}
</style>
