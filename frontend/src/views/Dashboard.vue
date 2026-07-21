<template>
  <section>
    <PageHeader title="运营工作台" description="先处理异常和待办，再观察全站业务规模；这里不展示管理员个人账户数据。" eyebrow="Operations">
      <template #actions><button class="button button-secondary" type="button" :disabled="loading" @click="load"><UiIcon name="refresh" />{{ loading ? '刷新中…' : '刷新工作台' }}</button></template>
    </PageHeader>
    <div v-if="error" class="alert alert-danger dashboard-alert"><UiIcon name="alert" />{{ error }}</div>

    <div class="metric-strip dashboard-section">
      <article v-for="metric in metrics" :key="metric.label" class="metric-item"><span class="metric-icon"><UiIcon :name="metric.icon" /></span><div><p class="metric-label">{{ metric.label }} · {{ metric.status }}</p><p class="metric-value">{{ metric.value }}</p><span class="metric-meta">{{ metric.meta }}</span></div></article>
    </div>

    <article class="workspace-section dashboard-section">
      <header class="panel-header"><div><h2>待办与异常</h2><p>按影响范围汇总需要管理员介入的事项。</p></div><span class="refresh-time">更新于 {{ refreshedAt }}</span></header>
      <div class="action-queue">
        <RouterLink v-for="item in actionQueue" :key="item.label" :to="item.to" :class="item.tone">
          <span class="queue-icon"><UiIcon :name="item.icon" /></span><div><strong>{{ item.value }}</strong><span>{{ item.label }}</span><p>{{ item.description }}</p></div><UiIcon name="chevron" />
        </RouterLink>
      </div>
    </article>

    <div class="section-grid dashboard-section">
      <article class="workspace-section span-7">
        <header class="panel-header"><div><h2>资源交付链路</h2><p>从主机接入到可售协议资源的实际准备状态。</p></div><strong class="progress-count">{{ completedReadiness }}/{{ readiness.length }}</strong></header>
        <div class="panel-body readiness-list">
          <RouterLink v-for="(item,index) in readiness" :key="item.label" :to="item.to" :class="{ complete: item.complete }"><span class="step-index"><UiIcon v-if="item.complete" name="check" /><template v-else>{{ index + 1 }}</template></span><div><strong>{{ item.label }}</strong><p>{{ item.description }}</p></div><StatusBadge :tone="item.complete ? 'success' : 'warning'">{{ item.complete ? '已就绪' : '待处理' }}</StatusBadge><UiIcon name="chevron" /></RouterLink>
        </div>
      </article>

      <article class="workspace-section span-5">
        <header class="panel-header"><div><h2>最近协议部署</h2><p>展示显式 SSH 部署结果；Zero 运行时校验与公网可达性仍需独立验证。</p></div><RouterLink class="button button-ghost button-sm" to="/admin/protocols">协议服务<UiIcon name="chevron" /></RouterLink></header>
        <div v-if="deployments.length" class="deployment-list"><div v-for="deployment in deployments" :key="deployment.id"><span class="deployment-dot" :class="deployment.status"></span><div><strong>端点 #{{ deployment.protocol_endpoint_id }}</strong><p>节点 #{{ deployment.node_id }} · {{ formatDateTime(deployment.created_at) }}</p></div><StatusBadge :tone="deploymentTone(deployment.status)">{{ deploymentLabel(deployment.status) }}</StatusBadge></div></div>
        <EmptyState v-else icon="nodes" title="暂无部署记录" description="从协议服务页面完成首次显式部署后，这里会展示结果。" />
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { fetchDashboard, fetchNodes, fetchProtocolDeployments, fetchProtocolEndpoints, type ProtocolDeployment } from '../api/client'
import EmptyState from '../components/EmptyState.vue'
import PageHeader from '../components/PageHeader.vue'
import StatusBadge from '../components/StatusBadge.vue'
import UiIcon from '../components/UiIcon.vue'
import { formatCurrency, formatDateTime, formatNumber } from '../utils/format'

const loading = ref(false)
const error = ref('')
const stats = ref<Record<string, number>>({})
const nodes = ref<any[]>([])
const endpoints = ref<any[]>([])
const deployments = ref<ProtocolDeployment[]>([])
const refreshedAt = ref('—')
const metrics = computed(() => [
  { label: '活跃用户', value: formatNumber(stats.value.users_active), icon: 'users', status: '全站', tone: 'info' as const, meta: `注册用户 ${formatNumber(stats.value.users)}` },
  { label: '有效订阅', value: formatNumber(stats.value.subscriptions_active), icon: 'plans', status: '服务中', tone: 'success' as const, meta: `全部订阅 ${formatNumber(stats.value.subscriptions)}` },
  { label: '已支付订单', value: formatNumber(stats.value.orders_paid), icon: 'billing', status: '累计', tone: 'info' as const, meta: `全部订单 ${formatNumber(stats.value.orders)}` },
  { label: '累计收入', value: formatCurrency(stats.value.revenue_cents, 'CNY'), icon: 'dollar', status: 'CNY', tone: 'info' as const, meta: '按已支付订单统计' }
])
const actionQueue = computed(() => [
  { label: '待管理员处理工单', value: Number(stats.value.tickets_pending) || 0, icon: 'ticket', tone: stats.value.tickets_pending ? 'warning' : 'normal', description: '新工单与等待管理员回复', to: '/admin/tickets' },
  { label: '待处理订单', value: (Number(stats.value.orders_pending) || 0) + (Number(stats.value.orders_failed) || 0), icon: 'billing', tone: stats.value.orders_failed ? 'danger' : stats.value.orders_pending ? 'warning' : 'normal', description: `${formatNumber(stats.value.orders_pending)} 待支付 · ${formatNumber(stats.value.orders_failed)} 失败`, to: '/admin/orders' },
  { label: '离线节点', value: Number(stats.value.nodes_offline) || 0, icon: 'nodes', tone: stats.value.nodes_offline ? 'danger' : 'normal', description: '可信运行信号超过两分钟', to: '/admin/nodes' },
  { label: '失败运营任务', value: Number(stats.value.tasks_failed) || 0, icon: 'tasks', tone: stats.value.tasks_failed ? 'danger' : 'normal', description: '可进入任务队列检查并重试', to: '/admin/tasks?status=3' },
  { label: '失败协议部署', value: Number(stats.value.deployments_failed) || 0, icon: 'alert', tone: stats.value.deployments_failed ? 'danger' : 'normal', description: 'SSH 部署失败记录', to: '/admin/protocols' }
])
const readiness = computed(() => [
  { label: '登记运行主机', description: `${nodes.value.length} 台主机已纳入平台`, complete: nodes.value.length > 0, to: '/admin/nodes' },
  { label: '建立 Zero 主动连接', description: `${nodes.value.filter(node => node.connector_online).length} 台在两分钟窗口内有心跳`, complete: nodes.value.some(node => node.connector_online), to: '/admin/nodes' },
  { label: '验证 SSH 运维通道', description: `${nodes.value.filter(node => node.ssh_verified_at && node.ssh_host_key_fingerprint).length} 台已验证认证凭证并固定主机身份`, complete: nodes.value.some(node => node.ssh_verified_at && node.ssh_host_key_fingerprint), to: '/admin/nodes' },
  { label: '配置流量上报凭证', description: `${nodes.value.filter(node => node.traffic_secret_prefix && !node.traffic_secret_revoked_at).length} 台可进行可信上报`, complete: nodes.value.some(node => node.traffic_secret_prefix && !node.traffic_secret_revoked_at), to: '/admin/nodes' },
  { label: '登记协议交付资源', description: `${endpoints.value.filter(endpoint => endpoint.is_active).length} 个业务启用端点；运行态仍需独立验证`, complete: endpoints.value.length > 0, to: '/admin/protocols' },
  { label: '建立销售与交付链路', description: `${formatNumber(stats.value.plans)} 个已发布商品`, complete: Number(stats.value.plans) > 0, to: '/admin/plans' }
])
const completedReadiness = computed(() => readiness.value.filter(item => item.complete).length)
function deploymentLabel(status: string) { return ({ running: '部署中', succeeded: '部署成功', failed: '部署失败' } as Record<string,string>)[status] || status }
function deploymentTone(status: string): 'info' | 'success' | 'danger' { return status === 'succeeded' ? 'success' : status === 'failed' ? 'danger' : 'info' }
async function load() { loading.value = true; error.value = ''; try { const [dashboardData,nodeData,endpointData,deploymentData] = await Promise.all([fetchDashboard(),fetchNodes(),fetchProtocolEndpoints(),fetchProtocolDeployments({ limit: 6 })]); stats.value = dashboardData; nodes.value = nodeData; endpoints.value = endpointData; deployments.value = deploymentData.items || []; refreshedAt.value = new Intl.DateTimeFormat('zh-CN',{hour:'2-digit',minute:'2-digit',second:'2-digit'}).format(new Date()) } catch (e:any) { error.value = e?.response?.data?.message || '运营工作台加载失败。' } finally { loading.value = false } }
onMounted(load)
</script>

<style scoped>
.dashboard-alert,.dashboard-section{margin-bottom:18px}.refresh-time{color:var(--muted);font-size:10px}.metric-strip{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));border-block:1px solid var(--line)}.metric-item{min-width:0;display:grid;grid-template-columns:auto minmax(0,1fr);align-items:center;gap:13px;padding:18px 20px}.metric-item+.metric-item{border-left:1px solid var(--line)}.metric-item .metric-icon{width:34px;height:34px;border-radius:8px}.metric-item .metric-label{margin:0}.metric-item .metric-value{font-size:25px}.action-queue{display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:0}.action-queue>a{min-width:0;display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:11px;padding:18px;text-decoration:none}.action-queue>a+a{border-left:1px solid var(--line)}.queue-icon{width:34px;height:34px;display:grid;place-items:center;border-radius:8px;color:var(--primary);background:var(--primary-soft)}.action-queue strong{display:block;font-size:22px}.action-queue span:not(.queue-icon){display:block;font-size:10px;font-weight:700}.action-queue p{margin:3px 0 0;color:var(--muted);font-size:9px}.action-queue>a>.ui-icon{color:var(--subtle)}.action-queue>a.warning .queue-icon{color:var(--warning);background:var(--warning-soft)}.action-queue>a.danger .queue-icon{color:var(--danger);background:var(--danger-soft)}.progress-count{color:var(--primary);font-size:14px}.readiness-list{display:grid}.readiness-list>a{display:grid;grid-template-columns:auto minmax(0,1fr) auto auto;align-items:center;gap:12px;padding:12px 4px;border-radius:0;text-decoration:none}.readiness-list>a+a{border-top:1px solid var(--line)}.readiness-list>a:hover{background:var(--surface-soft)}.step-index{width:30px;height:30px;display:grid;place-items:center;border-radius:50%;color:var(--warning);background:var(--warning-soft);font-size:11px;font-weight:700}.complete .step-index{color:var(--success);background:var(--success-soft)}.readiness-list strong{font-size:12px}.readiness-list p{margin:3px 0 0;color:var(--muted);font-size:10px}.readiness-list>a>.ui-icon{color:var(--subtle)}.deployment-list{display:grid}.deployment-list>div{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:10px;padding:13px 18px}.deployment-list>div+div{border-top:1px solid var(--line)}.deployment-dot{width:8px;height:8px;border-radius:50%;background:var(--info)}.deployment-dot.succeeded{background:var(--success)}.deployment-dot.failed{background:var(--danger)}.deployment-list strong{font-size:11px}.deployment-list p{margin:3px 0 0;color:var(--muted);font-size:9px}@media(max-width:1200px){.metric-strip{grid-template-columns:repeat(2,1fr)}.metric-item:nth-child(3){border-left:0}.metric-item:nth-child(n+3){border-top:1px solid var(--line)}.action-queue{grid-template-columns:repeat(2,1fr)}.action-queue>a+a{border-left:0;border-top:1px solid var(--line)}}@media(max-width:800px){.metric-strip,.action-queue{grid-template-columns:1fr}.metric-item+.metric-item{border-left:0;border-top:1px solid var(--line)}.readiness-list>a{grid-template-columns:auto minmax(0,1fr) auto}.readiness-list>a>.ui-icon{display:none}}
</style>
