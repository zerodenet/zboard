<template>
  <section>
    <PageHeader title="订单与订阅" description="从商品下单、订单确认到订阅交付和流量核对，集中处理完整交易链路。" eyebrow="Commerce">
      <template #actions><button class="button button-secondary" type="button" :disabled="loading" @click="loadData"><UiIcon name="refresh" />{{ loading ? '刷新中…' : '刷新数据' }}</button></template>
    </PageHeader>

    <div v-if="message" class="alert alert-success page-alert"><UiIcon name="check" />{{ message }}</div>
    <div v-if="errorMsg" class="alert alert-danger page-alert"><UiIcon name="alert" />{{ errorMsg }}</div>

    <nav class="workspace-tabs" aria-label="交易工作区">
      <button v-for="item in tabs" :key="item.key" type="button" :class="{ active: activeTab === item.key }" @click="activeTab = item.key"><UiIcon :name="item.icon" />{{ item.label }}<span v-if="item.count !== undefined">{{ item.count }}</span></button>
    </nav>

    <div v-if="activeTab === 'overview'" class="stack">
      <div class="metric-grid">
        <article class="metric-card"><div class="metric-top"><span class="metric-icon"><UiIcon name="activity" /></span><StatusBadge tone="info">流量</StatusBadge></div><div><p class="metric-label">剩余流量</p><p class="metric-value">{{ formatBytes(trafficSummary.remaining_bytes) }}</p><span class="metric-meta">累计使用 {{ formatBytes(trafficSummary.total_used_bytes) }}</span></div></article>
        <article class="metric-card"><div class="metric-top"><span class="metric-icon success"><UiIcon name="plans" /></span><StatusBadge :tone="activeSubscriptions.length ? 'success' : 'neutral'">有效</StatusBadge></div><div><p class="metric-label">有效订阅</p><p class="metric-value">{{ activeSubscriptions.length }}</p><span class="metric-meta">全部订阅 {{ subscriptions.length }}</span></div></article>
        <article class="metric-card"><div class="metric-top"><span class="metric-icon warning"><UiIcon name="clock" /></span><StatusBadge :tone="pendingOrders ? 'warning' : 'neutral'">待处理</StatusBadge></div><div><p class="metric-label">待支付订单</p><p class="metric-value">{{ pendingOrders }}</p><span class="metric-meta">全部订单 {{ orders.length }}</span></div></article>
        <article class="metric-card"><div class="metric-top"><span class="metric-icon"><UiIcon name="activity" /></span><StatusBadge tone="info">今日</StatusBadge></div><div><p class="metric-label">今日流量</p><p class="metric-value">{{ formatBytes(trafficSummary.used_bytes_today) }}</p><span class="metric-meta">统计时间 {{ formatDateTime(trafficSummary.as_of) }}</span></div></article>
      </div>

      <div class="section-grid">
        <article class="panel span-7">
          <header class="panel-header"><div><h2>有效订阅</h2><p>当前生效套餐的余额与到期时间。</p></div><button class="button button-ghost button-sm" type="button" @click="activeTab = 'subscriptions'">查看全部<UiIcon name="chevron" /></button></header>
          <div v-if="activeSubscriptions.length" class="panel-body subscription-cards">
            <article v-for="sub in activeSubscriptions" :key="sub.id">
              <div class="subscription-title"><div><span>订阅 #{{ sub.id }}</span><h3>{{ getPlanName(sub.plan_id) }}</h3></div><StatusBadge tone="success">服务中</StatusBadge></div>
              <p class="quota-value">{{ displayBytes(remainBytes(sub)) }} <span>/ {{ displayBytes(sub.flow_total) }}</span></p>
              <div class="usage-track"><i :style="{ width: `${usagePercent(sub)}%` }"></i></div>
              <footer><span>剩余 {{ daysUntilEnd(sub.end_at) }}</span><button class="button button-secondary button-sm" type="button" @click="order(sub.plan_sku_id, 'renewal', sub.id)"><UiIcon name="refresh" />续费同规格</button></footer>
            </article>
          </div>
          <EmptyState v-else icon="plans" title="暂无有效订阅" description="从右侧选择一个已发布套餐创建订单，支付完成后订阅会自动出现在这里。" />
        </article>

        <article class="panel span-5">
          <header class="panel-header"><div><h2>订阅链接</h2><p>用于客户端拉取当前有效协议配置。</p></div><StatusBadge :tone="subscriptionAccess.configured ? 'success' : 'warning'">{{ subscriptionAccess.configured ? '已启用' : '未生成' }}</StatusBadge></header>
          <div class="panel-body access-panel">
            <span class="access-icon"><UiIcon name="key" /></span>
            <div v-if="subscriptionAccess.configured" class="access-meta"><strong>链接凭证已配置</strong><p>前缀 <code>{{ subscriptionAccess.token_prefix }}…</code></p><p>最近使用：{{ formatDateTime(subscriptionAccess.last_used_at) }}</p></div>
            <div v-else class="access-meta"><strong>尚未生成订阅链接</strong><p>创建有效订阅后生成专属链接，并配置到客户端。</p></div>
            <label v-if="subscriptionUrl" class="field access-url"><span>仅本次显示</span><input :value="subscriptionUrl" readonly /></label>
            <div class="access-actions"><button v-if="subscriptionUrl" class="button button-secondary button-sm" type="button" @click="copySubscriptionUrl"><UiIcon name="copy" />{{ copyMessage || '复制链接' }}</button><button class="button button-secondary button-sm" type="button" @click="requestConfirm(subscriptionAccess.configured ? 'rotate' : 'generate')">{{ subscriptionAccess.configured ? '轮换链接' : '生成链接' }}</button><button v-if="subscriptionAccess.configured" class="button button-danger button-sm" type="button" @click="requestConfirm('revoke')">吊销</button></div>
          </div>
        </article>
      </div>

      <article class="panel">
        <header class="panel-header"><div><h2>可购买套餐</h2><p>选择销售规格创建新购订单。</p></div><RouterLink class="button button-ghost button-sm" to="/admin/plans">商品目录<UiIcon name="chevron" /></RouterLink></header>
        <div v-if="availableSkus.length" class="panel-body marketplace-grid">
          <article v-for="item in availableSkus" :key="item.sku.id">
            <div><span>{{ item.plan.name }}</span><h3>{{ item.sku.name }}</h3></div><p>{{ formatCurrency(item.sku.price_cents, item.sku.currency) }}<small>/ {{ billingLabel(item.sku) }}</small></p><ul><li>{{ displayBytes(item.sku.traffic_bytes) }} 流量</li><li>{{ item.sku.device_limit }} 台设备</li><li>{{ item.sku.speed_limit_mbps ? `${item.sku.speed_limit_mbps} Mbps` : '不限速' }}</li></ul><button class="button button-sm" type="button" @click="order(item.sku.id)">创建订单</button>
          </article>
        </div>
        <EmptyState v-else icon="plans" title="暂无可售套餐" description="管理员发布商品与 SKU 后会显示在这里。" />
      </article>
    </div>

    <article v-else-if="activeTab === 'orders'" class="panel">
      <header class="panel-header"><div><h2>订单记录</h2><p>查看订单快照、支付状态和人工确认。</p></div></header>
      <div class="panel-body toolbar"><div class="toolbar-group"><label class="field compact-field"><span>订单状态</span><select v-model="orderStatusFilter" @change="loadOrders"><option value="">全部状态</option><option value="pending">待支付</option><option value="paid">已支付</option><option value="failed">失败</option><option value="canceled">已取消</option></select></label></div><span class="muted">共 {{ orders.length }} 条记录</span></div>
      <div v-if="orders.length" class="table-shell"><table class="data-table"><thead><tr><th>订单</th><th>用户</th><th>商品规格</th><th>金额</th><th>状态</th><th></th></tr></thead><tbody><tr v-for="item in orders" :key="item.id"><td><div class="cell-title"><strong>#{{ item.id }}</strong><span class="mono">{{ item.trade_no }}</span></div></td><td>#{{ item.user_id }}</td><td><div class="cell-title"><strong>{{ item.plan_name || `套餐 #${item.plan_id}` }}</strong><span>{{ item.sku_name || '未知规格' }}</span></div></td><td>{{ formatCurrency(item.amount_cents, 'CNY') }}</td><td><StatusBadge :tone="orderTone(item.status)">{{ orderStatusName(item.status) }}</StatusBadge></td><td><div class="cell-actions"><button v-if="canCancel(item)" class="button button-ghost button-sm" type="button" @click="requestConfirm('cancel', item)">取消订单</button><button v-if="app.isAdmin && item.status !== 'paid'" class="button button-secondary button-sm" type="button" @click="requestConfirm('pay', item)">确认收款</button></div></td></tr></tbody></table></div>
      <EmptyState v-else icon="billing" title="没有匹配订单" description="调整筛选条件，或从可购买套餐创建一笔新订单。" />
    </article>

    <article v-else-if="activeTab === 'subscriptions'" class="panel">
      <header class="panel-header"><div><h2>订阅记录</h2><p>查看订阅状态、规格快照、流量与到期时间。</p></div><span class="count-label">{{ subscriptions.length }} 条</span></header>
      <div v-if="subscriptions.length" class="table-shell"><table class="data-table"><thead><tr><th>订阅</th><th>用户</th><th>套餐</th><th>流量使用</th><th>到期时间</th><th>状态</th><th></th></tr></thead><tbody><tr v-for="sub in subscriptions" :key="sub.id"><td>#{{ sub.id }}</td><td>#{{ sub.user_id }}</td><td><div class="cell-title"><strong>{{ getPlanName(sub.plan_id) }}</strong><span>SKU #{{ sub.plan_sku_id }}</span></div></td><td><div class="quota-cell"><span>{{ displayBytes(sub.flow_used) }} / {{ displayBytes(sub.flow_total) }}</span><div class="usage-track"><i :style="{ width: `${usagePercent(sub)}%` }"></i></div></div></td><td>{{ formatDateTime(sub.end_at) }}</td><td><StatusBadge :tone="subscriptionTone(sub.status)">{{ subscriptionStatusName(sub.status) }}</StatusBadge></td><td><div class="cell-actions"><button class="button button-secondary button-sm" type="button" @click="order(sub.plan_sku_id, 'renewal', sub.id)"><UiIcon name="refresh" />续费</button></div></td></tr></tbody></table></div>
      <EmptyState v-else icon="plans" title="暂无订阅" description="完成已支付订单后，系统会生成订阅记录。" />
    </article>

    <div v-else class="stack">
      <article class="panel">
        <header class="panel-header"><div><h2>流量明细</h2><p>按用户、节点、协议端点或订阅定位原始记录。</p></div></header>
        <div class="panel-body filter-grid"><label v-if="app.isAdmin" class="field"><span>用户 ID</span><input v-model.number="recordFilterUserId" type="number" min="1" placeholder="全部用户" /></label><label class="field"><span>节点 ID</span><input v-model.number="recordFilterNodeId" type="number" min="1" placeholder="全部节点" /></label><label class="field"><span>协议端点 ID</span><input v-model.number="recordFilterProtocolEndpointId" type="number" min="1" placeholder="全部端点" /></label><label class="field"><span>订阅 ID</span><input v-model.number="recordFilterSubscriptionId" type="number" min="1" placeholder="全部订阅" /></label><button class="button filter-button" type="button" @click="refreshTraffic"><UiIcon name="search" />查询</button></div>
        <div v-if="trafficRecords.length" class="table-shell"><table class="data-table"><thead><tr><th>记录</th><th>用户 / 订阅</th><th>节点 / 端点</th><th>原始流量</th><th>端点倍率</th><th>计费流量</th></tr></thead><tbody><tr v-for="record in trafficRecords" :key="record.id"><td>#{{ record.id }}</td><td>用户 #{{ record.user_id || '—' }}<br /><span class="muted">订阅 #{{ record.subscription_id || '—' }}</span></td><td>节点 #{{ record.node_id || '—' }}<br /><span class="muted">端点 #{{ record.protocol_endpoint_id || '—' }}</span></td><td>{{ displayBytes(record.raw_bytes) }}</td><td>{{ (record.protocol_multiplier_milli || 1000) / 1000 }}×</td><td><strong>{{ displayBytes(record.used_bytes) }}</strong></td></tr></tbody></table></div>
        <EmptyState v-else icon="activity" title="没有流量记录" description="当前筛选范围内尚未收到节点流量上报。" />
      </article>

      <article class="panel">
        <header class="panel-header"><div><h2>流量对账</h2><p>比较订阅累计用量与流量记录汇总，定位缺失或重复数据。</p></div></header>
        <div v-if="reconciliation.length" class="table-shell"><table class="data-table"><thead><tr><th>订阅</th><th>用户 / 套餐</th><th>订阅累计</th><th>记录汇总</th><th>差异</th><th>结论</th></tr></thead><tbody><tr v-for="item in reconciliation" :key="item.subscription_id"><td>#{{ item.subscription_id }}</td><td>用户 #{{ item.user_id }}<br /><span class="muted">套餐 #{{ item.plan_id }}</span></td><td>{{ displayBytes(item.flow_used) }}</td><td>{{ displayBytes(item.recorded_bytes) }}</td><td :class="item.difference ? 'danger-text' : 'success-text'">{{ displayBytes(Math.abs(item.difference || 0)) }}</td><td><StatusBadge :tone="reconciliationTone(item.result)">{{ reconciliationLabel(item.result) }}</StatusBadge></td></tr></tbody></table></div>
        <EmptyState v-else icon="shield" title="暂无对账结果" description="产生订阅流量后，对账结果会显示在这里。" />
      </article>
    </div>

    <ConfirmDialog :open="confirmOpen" :title="confirmTitle" :message="confirmMessage" :confirm-text="confirmButton" :tone="confirmKind === 'cancel' || confirmKind === 'revoke' ? 'danger' : 'primary'" :busy="confirmBusy" @close="confirmOpen = false" @confirm="executeConfirm" />
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { cancelOrder, createOrder, fetchOrders, fetchPlans, fetchSubscriptions, fetchSubscriptionAccess, fetchTrafficRecords, fetchTrafficReconciliation, fetchTrafficSummary, markOrderPaid, revokeSubscriptionAccess, rotateSubscriptionAccess } from '../api/client'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import EmptyState from '../components/EmptyState.vue'
import PageHeader from '../components/PageHeader.vue'
import StatusBadge from '../components/StatusBadge.vue'
import UiIcon from '../components/UiIcon.vue'
import { useAppStore } from '../stores/app'
import { daysRemaining, formatBytes, formatCurrency, formatDateTime } from '../utils/format'

type OrderItem = { id: number; user_id: number; plan_id: number; plan_name: string; sku_name: string; trade_no: string; status: string; amount_cents: number }
type SubscriptionItem = { id: number; user_id: number; plan_id: number; plan_sku_id: number; end_at: string; status: string; flow_total: number; flow_used: number }
const app = useAppStore()
const activeTab = ref<'overview' | 'orders' | 'subscriptions' | 'traffic'>('overview')
const loading = ref(false)
const orders = ref<OrderItem[]>([])
const subscriptions = ref<SubscriptionItem[]>([])
const plans = ref<any[]>([])
const subscriptionAccess = ref<any>({ configured: false })
const subscriptionUrl = ref('')
const copyMessage = ref('')
const trafficSummary = ref<Record<string, any>>({})
const trafficRecords = ref<any[]>([])
const reconciliation = ref<any[]>([])
const errorMsg = ref('')
const message = ref('')
const orderStatusFilter = ref('')
const recordFilterUserId = ref<number | undefined>()
const recordFilterNodeId = ref<number | undefined>()
const recordFilterProtocolEndpointId = ref<number | undefined>()
const recordFilterSubscriptionId = ref<number | undefined>()
const confirmOpen = ref(false)
const confirmBusy = ref(false)
const confirmKind = ref<'cancel' | 'pay' | 'generate' | 'rotate' | 'revoke'>('cancel')
const confirmTarget = ref<OrderItem | null>(null)
const activeSubscriptions = computed(() => subscriptions.value.filter(item => item.status === 'active'))
const pendingOrders = computed(() => orders.value.filter(item => item.status === 'pending').length)
const availableSkus = computed(() => plans.value.flatMap(plan => (plan.skus || []).filter((sku: any) => sku.is_active && plan.is_active).map((sku: any) => ({ plan, sku }))))
const tabs = computed(() => [
  { key: 'overview' as const, label: '概览', icon: 'dashboard' },
  { key: 'orders' as const, label: '订单', icon: 'billing', count: orders.value.length },
  { key: 'subscriptions' as const, label: '订阅', icon: 'plans', count: subscriptions.value.length },
  { key: 'traffic' as const, label: '流量与对账', icon: 'activity', count: trafficRecords.value.length }
])
const confirmTitle = computed(() => ({ cancel: '取消这笔订单？', pay: '确认订单已经收款？', generate: '生成订阅链接？', rotate: '轮换订阅链接？', revoke: '吊销订阅链接？' })[confirmKind.value])
const confirmMessage = computed(() => ({ cancel: '订单取消后无法继续支付；如需购买，需要重新创建订单。', pay: '人工确认收款会进入开通流程并生成或延长订阅，请确认资金已到账。', generate: '完整订阅链接只显示一次，请立即复制到客户端。', rotate: '旧订阅链接将立即失效，所有客户端都需要更新。', revoke: '吊销后客户端将无法继续通过当前链接拉取配置。' })[confirmKind.value])
const confirmButton = computed(() => ({ cancel: '确认取消', pay: '确认收款', generate: '生成链接', rotate: '确认轮换', revoke: '确认吊销' })[confirmKind.value])

function displayBytes(value: number) { return formatBytes(value) }
function remainBytes(sub: SubscriptionItem) { return Math.max(0, (sub.flow_total || 0) - (sub.flow_used || 0)) }
function usagePercent(sub: SubscriptionItem) { return sub.flow_total ? Math.min(100, Math.round((sub.flow_used || 0) / sub.flow_total * 100)) : 0 }
function daysUntilEnd(value: string) { return daysRemaining(value) }
function getPlanName(id: number) { return plans.value.find(item => item.id === id)?.name || `套餐 #${id}` }
function billingLabel(sku: any) { const unit = ({ day: '天', month: '月', year: '年', once: '次' } as Record<string, string>)[sku.billing_unit] || sku.billing_unit; return sku.billing_unit === 'once' ? '一次性' : `${sku.billing_value} ${unit}` }
function canCancel(item: OrderItem) { return item.status === 'pending' && (app.isAdmin || item.user_id === app.user.id) }
function orderTone(status: string): 'warning' | 'success' | 'danger' | 'neutral' { return status === 'paid' ? 'success' : status === 'pending' ? 'warning' : status === 'failed' ? 'danger' : 'neutral' }
function orderStatusName(status: string) { return ({ pending: '待支付', paid: '已支付', failed: '失败', canceled: '已取消' } as Record<string, string>)[status] || status }
function subscriptionTone(status: string): 'success' | 'warning' | 'danger' | 'neutral' { return status === 'active' ? 'success' : status === 'expired' ? 'warning' : status === 'exhausted' ? 'danger' : 'neutral' }
function subscriptionStatusName(status: string) { return ({ active: '有效', expired: '已到期', exhausted: '流量耗尽', inactive: '无效' } as Record<string, string>)[status] || status }
function reconciliationTone(result: string): 'success' | 'warning' | 'danger' | 'neutral' { return result === 'matched' ? 'success' : result === 'missing_records' || result === 'over_recorded' ? 'danger' : result === 'legacy' ? 'warning' : 'neutral' }
function reconciliationLabel(result: string) { return ({ matched: '一致', missing_records: '缺少记录', over_recorded: '记录过量', legacy: '历史数据' } as Record<string, string>)[result] || result }

async function loadOrders() { orders.value = await fetchOrders({ status: orderStatusFilter.value || undefined }, app.isAdmin) }
async function refreshTraffic() {
  [trafficRecords.value, reconciliation.value] = await Promise.all([
    fetchTrafficRecords({ userId: app.isAdmin ? recordFilterUserId.value : undefined, nodeId: recordFilterNodeId.value, protocolEndpointId: recordFilterProtocolEndpointId.value, subscriptionId: recordFilterSubscriptionId.value }, app.isAdmin),
    fetchTrafficReconciliation({ userId: app.isAdmin ? recordFilterUserId.value : undefined, subscriptionId: recordFilterSubscriptionId.value }, app.isAdmin)
  ])
}
async function loadData() {
  loading.value = true; errorMsg.value = ''; message.value = ''
  try {
    const [orderData, subscriptionData, accessData, planData, summaryData] = await Promise.all([fetchOrders({ status: orderStatusFilter.value || undefined }, app.isAdmin), fetchSubscriptions({}, app.isAdmin), fetchSubscriptionAccess(), fetchPlans(), fetchTrafficSummary(app.isAdmin)])
    orders.value = orderData; subscriptions.value = subscriptionData; subscriptionAccess.value = accessData; plans.value = planData; trafficSummary.value = summaryData
    await refreshTraffic()
  } catch (e: any) { errorMsg.value = e?.response?.data?.message || '交易数据加载失败。' }
  finally { loading.value = false }
}
async function order(skuId: number, orderType = 'new', targetSubscriptionId?: number) { try { await createOrder(skuId, { orderType, targetSubscriptionId }); message.value = orderType === 'renewal' ? '续费订单已创建。' : '新购订单已创建。'; await loadData(); activeTab.value = 'orders' } catch (e: any) { errorMsg.value = e?.response?.data?.message || '订单创建失败。' } }
function requestConfirm(kind: typeof confirmKind.value, target?: OrderItem) { confirmKind.value = kind; confirmTarget.value = target || null; confirmOpen.value = true }
async function executeConfirm() {
  confirmBusy.value = true; errorMsg.value = ''; message.value = ''
  try {
    if (confirmKind.value === 'cancel' && confirmTarget.value) { await cancelOrder(confirmTarget.value.id, app.isAdmin); message.value = '订单已取消。' }
    if (confirmKind.value === 'pay' && confirmTarget.value) { await markOrderPaid(confirmTarget.value.id); message.value = '订单已确认收款。' }
    if (confirmKind.value === 'generate' || confirmKind.value === 'rotate') { const result = await rotateSubscriptionAccess(); subscriptionAccess.value = result; subscriptionUrl.value = `${window.location.origin}${result.subscription_url}`; copyMessage.value = ''; message.value = '订阅链接已生成，请立即复制。' }
    if (confirmKind.value === 'revoke') { await revokeSubscriptionAccess(); subscriptionAccess.value = { configured: false }; subscriptionUrl.value = ''; message.value = '订阅链接已吊销。' }
    confirmOpen.value = false; if (['cancel', 'pay'].includes(confirmKind.value)) await loadData()
  } catch (e: any) { errorMsg.value = e?.response?.data?.message || '操作失败。' }
  finally { confirmBusy.value = false }
}
async function copySubscriptionUrl() { try { await navigator.clipboard.writeText(subscriptionUrl.value); copyMessage.value = '已复制' } catch { copyMessage.value = '复制失败' } }
onMounted(loadData)
</script>

<style scoped>
.page-alert { margin-bottom: 14px; }.workspace-tabs { display: flex; gap: 4px; margin-bottom: 16px; padding: 4px; overflow-x: auto; border: 1px solid var(--line); border-radius: 11px; background: var(--surface); }.workspace-tabs button { min-height: 38px; display: inline-flex; align-items: center; gap: 7px; padding: 7px 13px; border: 0; border-radius: 8px; color: var(--muted); background: transparent; font-size: 12px; font-weight: 650; white-space: nowrap; }.workspace-tabs button:hover { color: var(--text); background: var(--surface-soft); }.workspace-tabs button.active { color: var(--primary); background: var(--primary-soft); }.workspace-tabs button span { min-width: 19px; padding: 2px 5px; border-radius: 999px; color: inherit; background: rgb(102 112 133 / .09); font-size: 10px; }.metric-icon.success { color: var(--success); background: var(--success-soft); }.metric-icon.warning { color: var(--warning); background: var(--warning-soft); }.subscription-cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 12px; }.subscription-cards article { padding: 14px; border: 1px solid var(--line); border-radius: 10px; }.subscription-title { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; }.subscription-title span { color: var(--muted); font-size: 10px; }.subscription-title h3 { margin: 3px 0 0; font-size: 14px; }.quota-value { margin: 16px 0 8px; font-size: 21px; font-weight: 750; }.quota-value span { color: var(--muted); font-size: 11px; font-weight: 500; }.subscription-cards footer { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-top: 10px; color: var(--muted); font-size: 10px; }.access-panel { display: grid; gap: 13px; }.access-icon { width: 42px; height: 42px; display: grid; place-items: center; border-radius: 11px; color: var(--primary); background: var(--primary-soft); font-size: 20px; }.access-meta { display: grid; gap: 3px; }.access-meta strong { font-size: 14px; }.access-meta p { margin: 0; color: var(--muted); font-size: 11px; }.access-actions { display: flex; flex-wrap: wrap; gap: 7px; }.marketplace-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 12px; }.marketplace-grid article { display: grid; gap: 13px; padding: 16px; border: 1px solid var(--line); border-radius: 11px; }.marketplace-grid article > div span { color: var(--primary); font-size: 10px; font-weight: 700; }.marketplace-grid h3 { margin: 3px 0 0; font-size: 14px; }.marketplace-grid p { margin: 0; font-size: 22px; font-weight: 750; }.marketplace-grid p small { color: var(--muted); font-size: 10px; font-weight: 500; }.marketplace-grid ul { display: grid; gap: 5px; margin: 0; padding: 0; color: var(--muted); list-style: none; font-size: 11px; }.marketplace-grid li::before { content: '✓'; margin-right: 7px; color: var(--success); }.marketplace-grid .button { justify-self: start; }.compact-field { min-width: 160px; }.compact-field select { min-height: 34px; padding-block: 6px; }.count-label { color: var(--muted); font-size: 12px; }.quota-cell { min-width: 170px; }.quota-cell span { display: block; margin-bottom: 7px; font-size: 11px; }.quota-cell .usage-track { height: 5px; }.filter-grid { display: grid; grid-template-columns: repeat(4, minmax(130px, 1fr)) auto; align-items: end; gap: 10px; }.filter-button { margin-bottom: 0; }.success-text { color: var(--success); }.danger-text { color: var(--danger); }
@media (max-width: 1100px) { .filter-grid { grid-template-columns: repeat(2, 1fr); }.filter-button { align-self: end; } }
@media (max-width: 560px) { .filter-grid { grid-template-columns: 1fr; }.workspace-tabs { margin-inline: -4px; } }
</style>
