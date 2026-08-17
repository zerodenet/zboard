<template>
  <section class="standard-page">
    <PageHeader
      title="运营工作台"
      description="从经营结果、服务使用到当前未恢复异常查看全站状态；历史失败只保留在历史记录中。"
      eyebrow="Operations"
    >
      <template #actions>
        <div class="dashboard-controls">
          <div class="period-switch" aria-label="运营统计周期">
            <button
              v-for="option in periodOptions"
              :key="option.value"
              type="button"
              class="button button-sm"
              :class="selectedRange === option.value ? 'button-primary' : 'button-ghost'"
              :aria-pressed="selectedRange === option.value"
              @click="changeRange(option.value)"
            >
              {{ option.label }}
            </button>
          </div>
          <PageRefreshButton label="刷新运营工作台" :loading="loading" @click="load" />
        </div>
      </template>
    </PageHeader>

    <TransientFeedback :error="error" error-title="工作台加载失败" />

    <div v-if="overview" class="period-context">
      <span>{{ periodDescription }}</span>
      <span>查询时区 {{ overview.period.timezone }}</span>
      <TimeBadge :value="overview.as_of" mode="relative" />
    </div>

    <UiMetricStrip v-if="overview" class="dashboard-section business-metrics">
      <MetricCard
        v-for="metric in businessMetrics"
        :key="metric.label"
        :label="metric.label"
        :value="metric.value"
        :icon="metric.icon"
        :status="metric.status"
        tone="info"
        :meta="metric.meta"
      />
    </UiMetricStrip>

    <div v-if="overview" class="section-grid dashboard-section">
      <UiSection class="span-8" title="经营趋势" :description="trendDescription">
        <template #meta>
          <RouterLink class="button button-ghost button-sm" to="/admin/orders">
            订单明细<UiIcon name="chevron" />
          </RouterLink>
        </template>

        <div v-if="overview.trend.length" class="trend-shell">
          <div class="trend-summary">
            <div>
              <span>本期实收</span>
              <strong>{{ revenueValue }}</strong>
            </div>
            <div>
              <span>已支付订单</span>
              <strong>{{ formatNumber(overview.business.paid_orders) }}</strong>
            </div>
            <div>
              <span>构成</span>
              <strong>{{ formatNumber(overview.business.new_orders) }} 新购 · {{ formatNumber(overview.business.renew_orders) }} 续费</strong>
            </div>
          </div>

          <div class="trend-chart" role="img" :aria-label="trendAriaLabel">
            <div
              v-for="point in overview.trend"
              :key="point.bucket_start"
              class="trend-column"
              :title="trendPointTitle(point)"
            >
              <div class="trend-bar-track">
                <span class="trend-bar" :style="{ height: `${trendHeight(point)}%` }" />
              </div>
              <span class="trend-orders">{{ formatCompactCount(point.paid_orders) }}</span>
              <span class="trend-label">{{ trendBucketLabel(point.bucket_start) }}</span>
            </div>
          </div>

          <div class="trend-legend">
            <span><i class="legend-bar" />{{ overview.business.mixed_currency ? '柱高：订单量' : `柱高：实收金额 ${overview.business.currency || ''}` }}</span>
            <span>柱下数字：已支付订单数</span>
          </div>
        </div>
        <EmptyState v-else icon="billing" title="当前周期暂无已支付订单" description="产生已支付订单后，这里会按后端聚合周期展示经营趋势。" />
      </UiSection>

      <UiSection
        class="span-4"
        title="当前待办与异常"
        description="只展示现在仍需介入的状态；恢复后的失败不会继续占用待办。"
      >
        <div v-if="actionQueue.length" class="attention-list">
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

    <UiMetricStrip v-if="overview" class="dashboard-section service-metrics">
      <MetricCard
        label="当前活跃订阅"
        :value="observedValue(overview.service.active_subscriptions)"
        icon="plans"
        :status="overview.coverage.principal_flows ? '实时' : '未采集'"
        tone="info"
        :meta="overview.coverage.principal_flows ? '当前 active_flows > 0 的订阅' : '尚无 Principal 当前态观测样本'"
      />
      <MetricCard
        label="当前连接"
        :value="observedValue(overview.service.active_flows)"
        icon="nodes"
        :status="overview.coverage.principal_flows ? '实时' : '未采集'"
        tone="info"
        meta="跨节点 Subscription active flows 汇总"
      />
      <MetricCard
        label="本期流量"
        :value="formatBytes(overview.service.traffic_bytes)"
        icon="traffic"
        status="周期"
        tone="info"
        meta="按现有计费流量口径聚合"
      />
      <MetricCard
        label="在线节点"
        :value="`${formatNumber(overview.service.online_nodes)} / ${formatNumber(overview.service.enabled_nodes)}`"
        icon="nodes"
        status="当前"
        tone="info"
        meta="按 Connector 两分钟健康窗口"
      />
    </UiMetricStrip>

    <div v-if="overview" class="section-grid dashboard-section">
      <UiSection class="span-6" title="订阅健康" description="聚焦当前生命周期风险，不把普通支付失败等同于运营事故。">
        <div class="health-list">
          <RouterLink v-for="item in subscriptionHealth" :key="item.label" to="/admin/subscriptions" class="health-row">
            <span class="health-copy">
              <strong>{{ item.label }}</strong>
              <small>{{ item.description }}</small>
            </span>
            <span class="health-value">{{ formatNumber(item.value) }}</span>
            <StatusBadge :tone="item.tone">{{ item.status }}</StatusBadge>
            <UiIcon name="chevron" />
          </RouterLink>
        </div>
      </UiSection>

      <UiSection class="span-6" title="服务基础设施" description="展示当前运行和交付状态，而不是累计失败次数。">
        <div class="health-list">
          <RouterLink v-for="item in infrastructure" :key="item.label" :to="item.to" class="health-row">
            <span class="health-copy">
              <strong>{{ item.label }}</strong>
              <small>{{ item.description }}</small>
            </span>
            <span class="health-value">{{ item.value }}</span>
            <StatusBadge :tone="item.tone">{{ item.status }}</StatusBadge>
            <UiIcon name="chevron" />
          </RouterLink>
        </div>
      </UiSection>
    </div>

    <UiSection
      v-if="overview && !deliveryReady"
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

    <UiSection class="dashboard-section" title="最近重要运营事件" description="这里回答最近发生了什么；历史失败即使已恢复也可以保留，但不会进入当前待办。">
      <template #actions>
        <RouterLink class="button button-ghost button-sm" to="/admin/protocols">
          协议服务<UiIcon name="chevron" />
        </RouterLink>
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
      <EmptyState v-else icon="nodes" title="暂无重要运营事件" description="发生配置发布等高价值运营事件后会显示在这里。" />
    </UiSection>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { fetchProtocolDeployments, type ProtocolDeployment } from '../api/client'
import {
  fetchDashboardOverview,
  type DashboardOverview,
  type DashboardRange,
  type DashboardTrendPoint,
} from '../api/dashboard'
import DataTable from '../components/DataTable.vue'
import EmptyState from '../components/EmptyState.vue'
import MetricCard from '../components/MetricCard.vue'
import PageHeader from '../components/PageHeader.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TimeBadge from '../components/TimeBadge.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiIcon from '../components/UiIcon.vue'
import { formatBytes, formatCurrency, formatNumber, formatUnknownValue } from '../utils/format'
import { buildDashboardAttention } from '../utils/dashboardHealth'

const loading = ref(false)
const error = ref('')
const overview = ref<DashboardOverview | null>(null)
const deployments = ref<ProtocolDeployment[]>([])
const selectedRange = ref<DashboardRange>('7d')

const periodOptions: Array<{ label: string; value: DashboardRange }> = [
  { label: 'Today', value: 'today' },
  { label: '7 days', value: '7d' },
  { label: '30 days', value: '30d' },
]

const revenueValue = computed(() => {
  if (!overview.value) return '—'
  if (overview.value.business.mixed_currency) return '多币种'
  return formatCurrency(overview.value.business.revenue_cents, overview.value.business.currency || 'CNY')
})

const businessMetrics = computed(() => {
  if (!overview.value) return []
  const business = overview.value.business
  return [
    {
      label: '本期实收',
      value: revenueValue.value,
      icon: 'dollar',
      status: periodStatus.value,
      meta: business.mixed_currency
        ? '当前/上一周期包含多币种，金额不做跨币种比较'
        : comparisonText(business.revenue_cents, business.previous_revenue_cents, true),
    },
    {
      label: '已支付订单',
      value: formatNumber(business.paid_orders),
      icon: 'billing',
      status: periodStatus.value,
      meta: `${formatNumber(business.new_orders)} 新购 · ${formatNumber(business.renew_orders)} 续费 · ${comparisonText(business.paid_orders, business.previous_paid_orders)}`,
    },
    {
      label: '新增订阅',
      value: formatNumber(business.new_subscriptions),
      icon: 'plans',
      status: periodStatus.value,
      meta: comparisonText(business.new_subscriptions, business.previous_new_subscriptions),
    },
    {
      label: '续费成功',
      value: formatNumber(business.renew_orders),
      icon: 'refresh',
      status: periodStatus.value,
      meta: '仅展示已支付续费订单数，不虚构续费转化率',
    },
    {
      label: '有效订阅',
      value: formatNumber(business.active_subscriptions),
      icon: 'plans',
      status: '当前',
      meta: `${formatNumber(business.expiring_within_3d)} 个将在 3 天内到期`,
    },
  ]
})

const periodStatus = computed(() => ({ today: 'Today', '7d': '7 days', '30d': '30 days' }[selectedRange.value]))
const periodDescription = computed(() => overview.value ? `${formatUTC(overview.value.period.from)} – ${formatUTC(overview.value.period.to)}` : '')

const actionQueue = computed(() => {
  if (!overview.value) return []
  return buildDashboardAttention({
    nodes_offline: overview.value.attention.nodes_offline,
    deployments_failed: overview.value.attention.deployments_unresolved,
    tickets_pending: overview.value.attention.tickets_pending,
  })
})

const subscriptionHealth = computed(() => {
  if (!overview.value) return []
  const health = overview.value.subscriptions
  return [
    { label: '24 小时内到期', value: health.expiring_within_24h, status: health.expiring_within_24h > 0 ? '关注' : '正常', tone: health.expiring_within_24h > 0 ? 'warning' as const : 'success' as const, description: '仍有效且将在未来 24 小时内到期' },
    { label: '3 天内到期', value: health.expiring_within_3d, status: health.expiring_within_3d > 0 ? '关注' : '正常', tone: health.expiring_within_3d > 0 ? 'warning' as const : 'success' as const, description: '累计口径，包含 24 小时内到期订阅' },
    { label: '7 天内到期', value: health.expiring_within_7d, status: health.expiring_within_7d > 0 ? '观察' : '正常', tone: health.expiring_within_7d > 0 ? 'info' as const : 'success' as const, description: '用于提前观察未来一周订阅生命周期' },
    { label: '流量已耗尽', value: health.quota_exhausted, status: health.quota_exhausted > 0 ? '不可用' : '正常', tone: health.quota_exhausted > 0 ? 'danger' as const : 'success' as const, description: '有效期尚未结束但配额已经耗尽' },
  ]
})

const infrastructure = computed(() => {
  if (!overview.value) return []
  const infra = overview.value.infrastructure
  const offline = overview.value.attention.nodes_offline
  return [
    { label: '节点运行', value: `${formatNumber(infra.connector_online)} / ${formatNumber(infra.nodes_enabled)}`, status: offline > 0 ? '需处理' : '正常', tone: offline > 0 ? 'danger' as const : 'success' as const, description: 'Connector 当前在线 / 已启用节点', to: '/admin/nodes' },
    { label: 'SSH 运维通道', value: `${formatNumber(infra.ssh_verified)} 已验证`, status: infra.nodes_total > 0 && infra.ssh_verified < infra.nodes_total ? '未完整' : '已就绪', tone: infra.nodes_total > 0 && infra.ssh_verified < infra.nodes_total ? 'warning' as const : 'success' as const, description: '已验证凭证并固定主机身份的节点', to: '/admin/nodes' },
    { label: '流量上报凭证', value: `${formatNumber(infra.traffic_ready)} 已配置`, status: infra.nodes_total > 0 && infra.traffic_ready < infra.nodes_total ? '未完整' : '已就绪', tone: infra.nodes_total > 0 && infra.traffic_ready < infra.nodes_total ? 'warning' as const : 'success' as const, description: '这里只表示可信上报凭证就绪，不伪装成实时流量健康', to: '/admin/nodes' },
    { label: '协议交付', value: `${formatNumber(infra.active_protocol_endpoints)} / ${formatNumber(infra.protocol_endpoints)}`, status: infra.active_protocol_endpoints > 0 ? '可用' : '未就绪', tone: infra.active_protocol_endpoints > 0 ? 'success' as const : 'warning' as const, description: '已启用协议端点 / 全部协议端点', to: '/admin/protocols' },
    { label: '配置发布', value: infra.unresolved_deployments ? `${formatNumber(infra.unresolved_deployments)} 未恢复` : '已收敛', status: infra.unresolved_deployments ? '需处理' : '正常', tone: infra.unresolved_deployments ? 'danger' as const : 'success' as const, description: '只统计每个协议端点最新一次发布仍然失败的状态', to: '/admin/protocols?deployment=failed' },
  ]
})

const readiness = computed(() => {
  if (!overview.value) return []
  const infra = overview.value.infrastructure
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
const trendUsesRevenue = computed(() => Boolean(overview.value && !overview.value.business.mixed_currency))
const trendMax = computed(() => {
  if (!overview.value) return 1
  const values = overview.value.trend.map(point => trendUsesRevenue.value ? point.revenue_cents : point.paid_orders)
  return Math.max(1, ...values)
})
const trendDescription = computed(() => overview.value?.business.mixed_currency
  ? '检测到多个币种，避免错误相加金额；图中柱高改为已支付订单量，订单构成仍由后端聚合。'
  : 'Today 按小时、7/30 days 按日聚合实收金额；柱下同时显示已支付订单量。')
const trendAriaLabel = computed(() => overview.value ? `${periodStatus.value} 经营趋势，共 ${overview.value.trend.length} 个时间桶` : '经营趋势')

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
  return `${percent > 0 ? '+' : ''}${percent.toFixed(1)}% vs previous`
}

function observedValue(value: number | null) {
  return value === null ? '未采集' : formatNumber(value)
}

function trendHeight(point: DashboardTrendPoint) {
  const value = trendUsesRevenue.value ? point.revenue_cents : point.paid_orders
  if (value <= 0) return 0
  return Math.max(4, Math.min(100, (value / trendMax.value) * 100))
}

function trendPointTitle(point: DashboardTrendPoint) {
  const revenue = overview.value?.business.mixed_currency
    ? '多币种金额不合并展示'
    : formatCurrency(point.revenue_cents, overview.value?.business.currency || 'CNY')
  return `${formatUTC(point.bucket_start)} · 实收 ${revenue} · ${formatNumber(point.paid_orders)} 单（新购 ${formatNumber(point.new_orders)} / 续费 ${formatNumber(point.renew_orders)}）`
}

function trendBucketLabel(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  if (overview.value?.period.bucket === 'hour') return new Intl.DateTimeFormat('zh-CN', { timeZone: 'UTC', hour: '2-digit', minute: '2-digit', hour12: false }).format(date)
  return new Intl.DateTimeFormat('zh-CN', { timeZone: 'UTC', month: '2-digit', day: '2-digit' }).format(date)
}

function formatCompactCount(value: number) {
  if (value >= 1000) return `${(value / 1000).toFixed(value >= 10000 ? 0 : 1)}k`
  return String(value)
}

function formatUTC(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return `${new Intl.DateTimeFormat('zh-CN', { timeZone: 'UTC', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }).format(date)} UTC`
}

function deploymentLabel(status: string) {
  return ({ running: '部署中', succeeded: '部署成功', failed: '部署失败' } as Record<string, string>)[status] || formatUnknownValue('状态', status)
}
function deploymentTone(status: string): 'info' | 'success' | 'danger' {
  return status === 'succeeded' ? 'success' : status === 'failed' ? 'danger' : 'info'
}

async function changeRange(range: DashboardRange) {
  if (selectedRange.value === range) return
  selectedRange.value = range
  await load()
}
async function load() {
  loading.value = true
  error.value = ''
  try {
    const [dashboardData, deploymentData] = await Promise.all([
      fetchDashboardOverview(selectedRange.value),
      fetchProtocolDeployments({ limit: 8 }),
    ])
    overview.value = dashboardData
    deployments.value = deploymentData.items || []
  } catch (e: any) {
    error.value = e?.response?.data?.message || '运营工作台加载失败。'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.dashboard-section { margin-bottom: 18px; }
.dashboard-controls { display: flex; align-items: center; justify-content: flex-end; gap: 10px; flex-wrap: wrap; }
.period-switch { display: inline-flex; gap: 4px; padding: 3px; border: 1px solid var(--line); border-radius: 8px; }
.period-switch .button { min-width: 66px; }
.period-context { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin: -4px 0 14px; color: var(--muted); font-size: 10px; }
.business-metrics :deep(.metric-card) { min-width: 0; }
.trend-shell { min-width: 0; }
.trend-summary { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px; margin-bottom: 18px; }
.trend-summary > div { min-width: 0; padding: 10px 12px; border: 1px solid var(--line); border-radius: 8px; background: var(--surface-subtle); }
.trend-summary span { display: block; color: var(--muted); font-size: 9px; }
.trend-summary strong { display: block; margin-top: 4px; font-size: 13px; line-height: 1.3; overflow-wrap: anywhere; }
.trend-chart { height: 220px; display: flex; align-items: stretch; gap: clamp(3px, .7vw, 10px); padding: 8px 2px 0; border-bottom: 1px solid var(--line); overflow-x: auto; }
.trend-column { min-width: 18px; flex: 1 0 18px; display: grid; grid-template-rows: minmax(120px, 1fr) 18px 24px; align-items: end; text-align: center; }
.trend-bar-track { width: 70%; max-width: 34px; height: 100%; justify-self: center; display: flex; align-items: end; border-radius: 5px 5px 0 0; background: var(--surface-subtle); overflow: hidden; }
.trend-bar { width: 100%; min-height: 0; border-radius: 5px 5px 0 0; background: currentColor; color: var(--primary); opacity: .78; transition: height .18s ease; }
.trend-orders { align-self: center; color: var(--text); font-size: 9px; font-weight: 700; }
.trend-label { align-self: center; color: var(--muted); font-size: 8px; white-space: nowrap; }
.trend-legend { display: flex; gap: 16px; flex-wrap: wrap; margin-top: 10px; color: var(--muted); font-size: 9px; }
.trend-legend span { display: inline-flex; align-items: center; gap: 5px; }
.legend-bar { width: 9px; height: 9px; border-radius: 2px; background: var(--primary); opacity: .78; }
.attention-list, .health-list, .readiness-list { display: grid; }
.attention-list > a { min-width: 0; display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 12px; padding: 16px 4px; text-decoration: none; }
.attention-list > a + a, .health-row + .health-row, .readiness-list > a + a { border-top: 1px solid var(--line); }
.queue-icon { width: 36px; height: 36px; display: grid; place-items: center; border-radius: 8px; color: var(--warning); background: var(--warning-soft); }
.attention-list > a.danger .queue-icon { color: var(--danger); background: var(--danger-soft); }
.queue-copy { min-width: 0; }
.queue-label { display: block; font-size: 11px; font-weight: 700; }
.queue-copy strong { display: block; margin-top: 2px; font-size: 22px; line-height: 1.1; }
.queue-copy p { margin: 4px 0 0; color: var(--muted); font-size: 10px; }
.attention-list > a > .ui-icon { color: var(--subtle); }
.health-row { display: grid; grid-template-columns: minmax(0, 1fr) auto auto auto; align-items: center; gap: 10px; padding: 13px 4px; text-decoration: none; }
.health-copy { min-width: 0; }
.health-copy strong { display: block; font-size: 11px; }
.health-copy small { display: block; margin-top: 3px; color: var(--muted); font-size: 9px; line-height: 1.4; }
.health-value { font-size: 12px; font-weight: 700; white-space: nowrap; }
.health-row > .ui-icon { color: var(--subtle); }
.progress-count { font-size: 11px; }
.readiness-list > a { display: grid; grid-template-columns: auto minmax(0, 1fr) auto auto; gap: 12px; align-items: center; padding: 13px 4px; text-decoration: none; }
.readiness-list strong { display: block; font-size: 11px; }
.readiness-list p { margin: 3px 0 0; color: var(--muted); font-size: 9px; }
.step-index { width: 27px; height: 27px; display: grid; place-items: center; border-radius: 50%; background: var(--warning-soft); color: var(--warning); font-size: 10px; font-weight: 800; }
.readiness-list > a.complete .step-index { background: var(--success-soft); color: var(--success); }
@media (max-width: 900px) {
  .dashboard-controls { justify-content: flex-start; }
  .period-switch { width: 100%; }
  .period-switch .button { flex: 1; }
  .trend-summary { grid-template-columns: 1fr; }
}
@media (max-width: 640px) {
  .health-row { grid-template-columns: minmax(0, 1fr) auto auto; }
  .health-row > .ui-icon { display: none; }
  .readiness-list > a { grid-template-columns: auto minmax(0, 1fr) auto; }
  .readiness-list > a > .ui-icon { display: none; }
  .trend-chart { height: 190px; }
}
</style>
