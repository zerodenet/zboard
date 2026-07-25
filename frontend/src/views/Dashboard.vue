<template>
  <section class="standard-page">
    <PageHeader title="运营工作台" description="先处理异常和待办，再观察全站业务规模；这里不展示管理员个人账户数据。" eyebrow="Operations">
      <template #actions><PageRefreshButton label="刷新运营工作台" :loading="loading" @click="load" /></template>
    </PageHeader>
    <TransientFeedback :error="error" error-title="工作台加载失败" />

    <UiMetricStrip class="dashboard-section">
      <MetricCard v-for="metric in metrics" :key="metric.label" :label="metric.label" :value="metric.value" :icon="metric.icon" :status="metric.status" tone="info" :meta="metric.meta" />
    </UiMetricStrip>

    <UiSection class="dashboard-section" title="待办与异常" description="按影响范围汇总需要管理员介入的事项。">
      <template #meta><TimeBadge :value="refreshedAt" mode="relative" /></template>
      <div class="action-queue">
        <RouterLink v-for="item in actionQueue" :key="item.label" :to="item.to" :class="item.tone">
          <span class="queue-icon"><UiIcon :name="item.icon" /></span><div><strong>{{ item.value }}</strong><span>{{ item.label }}</span><p>{{ item.description }}</p></div><UiIcon name="chevron" />
        </RouterLink>
      </div>
    </UiSection>

    <div class="section-grid dashboard-section">
      <UiSection class="span-7" title="资源交付链路" description="从主机接入到可售协议资源的实际准备状态。">
        <template #meta><strong class="progress-count">{{ completedReadiness }}/{{ readiness.length }}</strong></template>
        <div class="panel-body readiness-list">
          <RouterLink v-for="(item,index) in readiness" :key="item.label" :to="item.to" :class="{ complete: item.complete }"><span class="step-index"><UiIcon v-if="item.complete" name="check" /><template v-else>{{ index + 1 }}</template></span><div><strong>{{ item.label }}</strong><p>{{ item.description }}</p></div><StatusBadge :tone="item.complete ? 'success' : 'warning'">{{ item.complete ? '已就绪' : '待处理' }}</StatusBadge><UiIcon name="chevron" /></RouterLink>
        </div>
      </UiSection>

      <UiSection class="span-5" title="最近配置发布" description="展示节点完整配置的自动发布结果；公网可达性仍需独立验证。">
        <template #actions><RouterLink class="button button-ghost button-sm" to="/admin/protocols">协议服务<UiIcon name="chevron" /></RouterLink></template>
        <DataTable v-if="deployments.length" caption="最近协议配置发布结果" :row-count="deployments.length" :min-width="500" table-class="dashboard-deployment-table">
          <thead><tr><th class="table-primary-column">协议端点</th><th>节点</th><th>状态</th><th data-column-priority="2">时间</th></tr></thead>
          <tbody><tr v-for="deployment in deployments" :key="deployment.id"><td class="table-primary-column"><strong>#{{ deployment.protocol_endpoint_id }}</strong></td><td class="numeric-column">#{{ deployment.node_id }}</td><td><StatusBadge :tone="deploymentTone(deployment.status)">{{ deploymentLabel(deployment.status) }}</StatusBadge></td><td data-column-priority="2"><TimeBadge :value="deployment.created_at" /></td></tr></tbody>
        </DataTable>
        <EmptyState v-else icon="nodes" title="暂无发布记录" description="保存协议服务并完成首次自动发布后，这里会展示结果。" />
      </UiSection>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { fetchDashboard, fetchProtocolDeployments, type ProtocolDeployment } from '../api/client'
import DataTable from '../components/DataTable.vue'
import EmptyState from '../components/EmptyState.vue'
import MetricCard from '../components/MetricCard.vue'
import PageHeader from '../components/PageHeader.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TimeBadge from '../components/TimeBadge.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiIcon from '../components/UiIcon.vue'
import { formatCurrency, formatNumber, formatUnknownValue } from '../utils/format'
import { dashboardActionRoutes } from '../utils/dashboardActions'

const loading = ref(false)
const error = ref('')
const stats = ref<Record<string, number>>({})
const deployments = ref<ProtocolDeployment[]>([])
const refreshedAt = ref<string | null>(null)
const metrics = computed(() => [
  { label: '活跃用户', value: formatNumber(stats.value.users_active), icon: 'users', status: '全站', tone: 'info' as const, meta: `注册用户 ${formatNumber(stats.value.users)}` },
  { label: '有效订阅', value: formatNumber(stats.value.subscriptions_active), icon: 'plans', status: '服务中', tone: 'success' as const, meta: `全部订阅 ${formatNumber(stats.value.subscriptions)}` },
  { label: '已支付订单', value: formatNumber(stats.value.orders_paid), icon: 'billing', status: '累计', tone: 'info' as const, meta: `全部订单 ${formatNumber(stats.value.orders)}` },
  { label: '累计收入', value: formatCurrency(stats.value.revenue_cents, 'CNY'), icon: 'dollar', status: 'CNY', tone: 'info' as const, meta: '按已支付订单统计' }
])
const actionQueue = computed(() => [
  { label: '待管理员处理工单', value: Number(stats.value.tickets_pending) || 0, icon: 'ticket', tone: stats.value.tickets_pending ? 'warning' : 'normal', description: '新工单与等待管理员回复', to: dashboardActionRoutes.tickets },
  { label: '待处理订单', value: (Number(stats.value.orders_pending) || 0) + (Number(stats.value.orders_failed) || 0), icon: 'billing', tone: stats.value.orders_failed ? 'danger' : stats.value.orders_pending ? 'warning' : 'normal', description: `${formatNumber(stats.value.orders_pending)} 待支付 · ${formatNumber(stats.value.orders_failed)} 失败`, to: dashboardActionRoutes.orders },
  { label: '离线节点', value: Number(stats.value.nodes_offline) || 0, icon: 'nodes', tone: stats.value.nodes_offline ? 'danger' : 'normal', description: '可信运行信号超过两分钟', to: dashboardActionRoutes.nodes },
  { label: '失败运营任务', value: Number(stats.value.tasks_failed) || 0, icon: 'tasks', tone: stats.value.tasks_failed ? 'danger' : 'normal', description: '可进入任务队列检查并重试', to: dashboardActionRoutes.tasks },
  { label: '失败协议部署', value: Number(stats.value.deployments_failed) || 0, icon: 'alert', tone: stats.value.deployments_failed ? 'danger' : 'normal', description: '当前版本发布失败，需要检查或重新发布', to: dashboardActionRoutes.deployments }
])
const readiness = computed(() => [
  { label: '登记运行主机', description: `${formatNumber(stats.value.nodes)} 台主机已纳入平台`, complete: Number(stats.value.nodes) > 0, to: '/admin/nodes' },
  { label: '建立 Zero 主动连接', description: `${formatNumber(stats.value.nodes_connector_online)} 台在两分钟窗口内有心跳`, complete: Number(stats.value.nodes_connector_online) > 0, to: '/admin/nodes' },
  { label: '验证 SSH 运维通道', description: `${formatNumber(stats.value.nodes_ssh_verified)} 台已验证凭证并固定主机身份`, complete: Number(stats.value.nodes_ssh_verified) > 0, to: '/admin/nodes' },
  { label: '配置流量上报凭证', description: `${formatNumber(stats.value.nodes_traffic_ready)} 台可进行可信上报`, complete: Number(stats.value.nodes_traffic_ready) > 0, to: '/admin/nodes' },
  { label: '登记协议交付资源', description: `${formatNumber(stats.value.protocol_endpoints_active)} 个已启用，共 ${formatNumber(stats.value.protocol_endpoints)} 个端点`, complete: Number(stats.value.protocol_endpoints) > 0, to: '/admin/protocols' },
  { label: '建立销售与交付链路', description: `${formatNumber(stats.value.plans)} 个已发布商品`, complete: Number(stats.value.plans) > 0, to: '/admin/plans' }
])
const completedReadiness = computed(() => readiness.value.filter(item => item.complete).length)
function deploymentLabel(status: string) { return ({ running: '部署中', succeeded: '部署成功', failed: '部署失败' } as Record<string,string>)[status] || formatUnknownValue('状态', status) }
function deploymentTone(status: string): 'info' | 'success' | 'danger' { return status === 'succeeded' ? 'success' : status === 'failed' ? 'danger' : 'info' }
async function load() { loading.value = true; error.value = ''; try { const [dashboardData,deploymentData] = await Promise.all([fetchDashboard(),fetchProtocolDeployments({ limit: 6 })]); stats.value = dashboardData; deployments.value = deploymentData.items || []; refreshedAt.value = new Date().toISOString() } catch (e:any) { error.value = e?.response?.data?.message || '运营工作台加载失败。' } finally { loading.value = false } }
onMounted(load)
</script>

<style scoped>
.dashboard-alert,.dashboard-section{margin-bottom:18px}.refresh-time{color:var(--muted);font-size:10px}.metric-strip{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));border-block:1px solid var(--line)}.metric-item{min-width:0;display:grid;grid-template-columns:auto minmax(0,1fr);align-items:center;gap:13px;padding:18px 20px}.metric-item+.metric-item{border-left:1px solid var(--line)}.metric-item .metric-icon{width:34px;height:34px;border-radius:8px}.metric-item .metric-label{margin:0}.metric-item .metric-value{font-size:25px}.action-queue{display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:0}.action-queue>a{min-width:0;display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:11px;padding:18px;text-decoration:none}.action-queue>a+a{border-left:1px solid var(--line)}.queue-icon{width:34px;height:34px;display:grid;place-items:center;border-radius:8px;color:var(--primary);background:var(--primary-soft)}.action-queue strong{display:block;font-size:22px}.action-queue span:not(.queue-icon){display:block;font-size:10px;font-weight:700}.action-queue p{margin:3px 0 0;color:var(--muted);font-size:9px}.action-queue>a>.ui-icon{color:var(--subtle)}.action-queue>a.warning .queue-icon{color:var(--warning);background:var(--warning-soft)}.action-queue>a.danger .queue-icon{color:var(--danger);background:var(--danger-soft)}.progress-count{color:var(--primary);font-size:14px}.readiness-list{display:grid}.readiness-list>a{display:grid;grid-template-columns:auto minmax(0,1fr) auto auto;align-items:center;gap:12px;padding:12px 4px;border-radius:0;text-decoration:none}.readiness-list>a+a{border-top:1px solid var(--line)}.readiness-list>a:hover{background:var(--surface-soft)}.step-index{width:30px;height:30px;display:grid;place-items:center;border-radius:50%;color:var(--warning);background:var(--warning-soft);font-size:11px;font-weight:700}.complete .step-index{color:var(--success);background:var(--success-soft)}.readiness-list strong{font-size:12px}.readiness-list p{margin:3px 0 0;color:var(--muted);font-size:10px}.readiness-list>a>.ui-icon{color:var(--subtle)}.dashboard-deployment-table :deep(th),.dashboard-deployment-table :deep(td){padding-inline:12px}@media(max-width:1200px){.metric-strip{grid-template-columns:repeat(2,1fr)}.metric-item:nth-child(3){border-left:0}.metric-item:nth-child(n+3){border-top:1px solid var(--line)}.action-queue{grid-template-columns:repeat(2,1fr)}.action-queue>a+a{border-left:0;border-top:1px solid var(--line)}}@media(max-width:800px){.metric-strip,.action-queue{grid-template-columns:1fr}.metric-item+.metric-item{border-left:0;border-top:1px solid var(--line)}.readiness-list>a{grid-template-columns:auto minmax(0,1fr) auto}.readiness-list>a>.ui-icon{display:none}}
</style>
