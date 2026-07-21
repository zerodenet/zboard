<template>
  <section>
    <PageHeader title="流量与对账" description="按真实业务对象筛选流量记录，并将订阅累计与上报记录差异作为异常队列处理。" eyebrow="Usage Operations">
      <template #actions><button class="button button-secondary" type="button" :disabled="loading" @click="load"><UiIcon name="refresh" />{{ loading ? '查询中…' : '刷新数据' }}</button></template>
    </PageHeader>
    <div v-if="error" class="alert alert-danger page-alert"><UiIcon name="alert" />{{ error }}</div>

    <div class="metric-grid traffic-metrics">
      <article v-for="metric in metrics" :key="metric.label" class="metric-card"><div class="metric-top"><span class="metric-icon"><UiIcon :name="metric.icon" /></span><StatusBadge :tone="metric.tone">{{ metric.status }}</StatusBadge></div><div><p class="metric-label">{{ metric.label }}</p><p class="metric-value">{{ metric.value }}</p><span class="metric-meta">{{ metric.meta }}</span></div></article>
    </div>

    <article class="panel filter-panel"><div class="panel-body filter-grid"><label class="field"><span>用户</span><select v-model="filters.userId"><option value="">全部用户</option><option v-for="user in users" :key="user.id" :value="String(user.id)">{{ user.email }}</option></select></label><label class="field"><span>节点</span><select v-model="filters.nodeId"><option value="">全部节点</option><option v-for="node in nodes" :key="node.id" :value="String(node.id)">{{ node.name }}</option></select></label><label class="field"><span>协议端点</span><select v-model="filters.endpointId"><option value="">全部端点</option><option v-for="endpoint in endpoints" :key="endpoint.id" :value="String(endpoint.id)">{{ endpoint.name }}</option></select></label><label class="field"><span>订阅 ID</span><input v-model="filters.subscriptionId" inputmode="numeric" placeholder="全部订阅" /></label><button class="button filter-button" type="button" @click="load"><UiIcon name="search" />应用筛选</button></div></article>

    <div class="section-grid traffic-workspace">
      <article class="panel span-7"><header class="panel-header"><div><h2>上报记录</h2><p>展示节点上报的原始流量、协议端点倍率快照和最终计费量。</p></div><span class="count-label">{{ records.length }} 条</span></header><div v-if="records.length" class="table-shell"><table class="data-table"><thead><tr><th>时间</th><th>用户 / 订阅</th><th>节点 / 端点</th><th>原始</th><th>端点倍率</th><th>计费</th></tr></thead><tbody><tr v-for="record in records" :key="record.id"><td>{{ formatDateTime(record.record_at) }}</td><td><div class="cell-title"><strong>{{ userName(record.user_id) }}</strong><span>订阅 #{{ record.subscription_id || '—' }}</span></div></td><td><div class="cell-title"><strong>{{ nodeName(record.node_id) }}</strong><span>{{ endpointName(record.protocol_endpoint_id) }}</span></div></td><td>{{ formatBytes(record.raw_bytes) }}</td><td>{{ ((record.protocol_multiplier_milli || 1000) / 1000).toFixed(2) }}×</td><td><strong>{{ formatBytes(record.used_bytes) }}</strong></td></tr></tbody></table></div><EmptyState v-else icon="activity" title="没有流量记录" description="当前筛选范围内尚未收到节点上报。" /></article>

      <article class="panel span-5"><header class="panel-header"><div><h2>对账异常</h2><p>只突出需要运营处理的缺失或超额记录。</p></div><StatusBadge :tone="issues.length ? 'danger' : 'success'">{{ issues.length ? `${issues.length} 项异常` : '全部一致' }}</StatusBadge></header><div v-if="issues.length" class="reconciliation-list"><article v-for="item in issues" :key="item.subscription_id"><div><strong>订阅 #{{ item.subscription_id }}</strong><StatusBadge :tone="resultTone(item.result)">{{ resultName(item.result) }}</StatusBadge></div><p>{{ userName(item.user_id) }} · {{ planName(item.plan_id) }}</p><dl><div><dt>订阅累计</dt><dd>{{ formatBytes(item.flow_used) }}</dd></div><div><dt>记录汇总</dt><dd>{{ formatBytes(item.recorded_bytes) }}</dd></div><div><dt>差异</dt><dd class="danger-text">{{ formatBytes(Math.abs(item.difference || 0)) }}</dd></div></dl><RouterLink class="button button-secondary button-sm" :to="`/admin/subscriptions?user_id=${item.user_id}`">查看用户订阅</RouterLink></article></div><EmptyState v-else icon="shield" title="当前没有对账异常" description="订阅累计用量与上报记录汇总一致。" /></article>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { fetchNodes, fetchPlans, fetchProtocolEndpoints, fetchTrafficReconciliation, fetchTrafficRecords, fetchUsers } from '../api/client'
import EmptyState from '../components/EmptyState.vue'
import PageHeader from '../components/PageHeader.vue'
import StatusBadge from '../components/StatusBadge.vue'
import UiIcon from '../components/UiIcon.vue'
import { formatBytes, formatDateTime } from '../utils/format'

const route = useRoute()
const records = ref<any[]>([])
const reconciliation = ref<any[]>([])
const users = ref<any[]>([])
const nodes = ref<any[]>([])
const endpoints = ref<any[]>([])
const plans = ref<any[]>([])
const filters = reactive({ userId: String(route.query.user_id || ''), nodeId: String(route.query.node_id || ''), endpointId: String(route.query.protocol_endpoint_id || ''), subscriptionId: String(route.query.subscription_id || '') })
const loading = ref(false)
const error = ref('')
const issues = computed(() => reconciliation.value.filter(item => item.result !== 'matched'))
const metrics = computed(() => [
  { label: '原始流量', value: formatBytes(records.value.reduce((sum, item) => sum + (item.raw_bytes || 0), 0)), icon: 'activity', status: '上报', tone: 'info' as const, meta: '当前记录原始流量合计' },
  { label: '计费流量', value: formatBytes(records.value.reduce((sum, item) => sum + (item.used_bytes || 0), 0)), icon: 'database', status: '扣减', tone: 'info' as const, meta: '应用协议端点倍率后' },
  { label: '涉及订阅', value: new Set(records.value.map(item => item.subscription_id).filter(Boolean)).size, icon: 'plans', status: '范围', tone: 'neutral' as const, meta: '当前记录关联的订阅' },
  { label: '对账异常', value: issues.value.length, icon: 'alert', status: issues.value.length ? '需处理' : '正常', tone: issues.value.length ? 'danger' as const : 'success' as const, meta: '缺失、超额或历史记录' }
])
function userName(id: number) { return users.value.find(item => item.id === id)?.email || `用户 #${id || '—'}` }
function nodeName(id: number) { return nodes.value.find(item => item.id === id)?.name || `节点 #${id || '—'}` }
function endpointName(id: number) { return endpoints.value.find(item => item.id === id)?.name || `端点 #${id || '—'}` }
function planName(id: number) { return plans.value.find(item => item.id === id)?.name || `套餐 #${id}` }
function resultName(result: string) { return ({ matched: '一致', missing_records: '缺少记录', over_recorded: '记录超额', legacy: '历史数据' } as Record<string, string>)[result] || result }
function resultTone(result: string): 'success' | 'warning' | 'danger' { return result === 'matched' ? 'success' : result === 'legacy' ? 'warning' : 'danger' }
async function load() { loading.value = true; error.value = ''; const params = { userId: Number(filters.userId) || undefined, nodeId: Number(filters.nodeId) || undefined, protocolEndpointId: Number(filters.endpointId) || undefined, subscriptionId: Number(filters.subscriptionId) || undefined }; try { const [recordData, reconciliationData, userData, nodeData, endpointData, planData] = await Promise.all([fetchTrafficRecords(params, true), fetchTrafficReconciliation({ userId: params.userId, subscriptionId: params.subscriptionId }, true), fetchUsers(), fetchNodes(), fetchProtocolEndpoints(), fetchPlans()]); records.value = recordData; reconciliation.value = reconciliationData; users.value = userData; nodes.value = nodeData; endpoints.value = endpointData; plans.value = planData } catch (e: any) { error.value = e?.response?.data?.message || '流量与对账数据加载失败。' } finally { loading.value = false } }
onMounted(load)
</script>

<style scoped>
.page-alert,.traffic-metrics,.filter-panel { margin-bottom: 16px; }.filter-grid { display: grid; grid-template-columns: repeat(4,minmax(150px,1fr)) auto; align-items: end; gap: 10px; }.filter-button { margin-bottom: 0; }.traffic-workspace { align-items: start; }.count-label { color: var(--muted); font-size: 12px; }.reconciliation-list { display: grid; gap: 0; }.reconciliation-list article { padding: 16px 18px; }.reconciliation-list article + article { border-top: 1px solid var(--line); }.reconciliation-list article > div:first-child { display: flex; align-items: center; justify-content: space-between; gap: 10px; }.reconciliation-list p { margin: 5px 0 13px; color: var(--muted); font-size: 10px; }.reconciliation-list dl { display: grid; gap: 7px; margin: 0 0 13px; }.reconciliation-list dl div { display: flex; justify-content: space-between; gap: 12px; }.reconciliation-list dt { color: var(--muted); font-size: 10px; }.reconciliation-list dd { margin: 0; font-size: 10px; font-weight: 650; }.danger-text { color: var(--danger); } @media(max-width:1100px){.filter-grid{grid-template-columns:repeat(2,1fr)}.traffic-workspace>.span-7,.traffic-workspace>.span-5{grid-column:span 12}} @media(max-width:560px){.filter-grid{grid-template-columns:1fr}}
</style>
