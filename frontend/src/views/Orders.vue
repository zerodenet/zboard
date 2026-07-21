<template>
  <section>
    <PageHeader title="订单管理" description="处理全站交易状态和人工收款，不混入管理员自己的购买与订阅操作。" eyebrow="Commerce">
      <template #actions><button class="button button-secondary" type="button" :disabled="loading" @click="load"><UiIcon name="refresh" />{{ loading ? '刷新中…' : '刷新订单' }}</button></template>
    </PageHeader>

    <div v-if="message" class="alert alert-success page-alert"><UiIcon name="check" />{{ message }}</div>
    <div v-if="error" class="alert alert-danger page-alert"><UiIcon name="alert" />{{ error }}</div>

    <div class="metric-grid order-metrics">
      <article v-for="metric in metrics" :key="metric.label" class="metric-card"><div class="metric-top"><span class="metric-icon"><UiIcon :name="metric.icon" /></span><StatusBadge :tone="metric.tone">{{ metric.status }}</StatusBadge></div><div><p class="metric-label">{{ metric.label }}</p><p class="metric-value">{{ metric.value }}</p><span class="metric-meta">{{ metric.meta }}</span></div></article>
    </div>

    <article class="panel">
      <header class="panel-header"><div><h2>交易队列</h2><p>优先处理待支付和失败订单；已取消订单不会再显示收款操作。</p></div><span class="count-label">{{ orders.length }} 笔</span></header>
      <div class="panel-body toolbar">
        <div class="toolbar-group">
          <select v-model="statusFilter" aria-label="订单状态" @change="load"><option value="">全部状态</option><option value="pending">待支付</option><option value="paid">已支付</option><option value="failed">失败</option><option value="canceled">已取消</option></select>
          <select v-model="userFilter" aria-label="订单用户" @change="load"><option value="">全部用户</option><option v-for="user in users" :key="user.id" :value="String(user.id)">{{ user.email }}</option></select>
        </div>
        <RouterLink class="button button-ghost button-sm" to="/admin/plans">管理商品<UiIcon name="chevron" /></RouterLink>
      </div>
      <div v-if="orders.length" class="table-shell"><table class="data-table"><thead><tr><th>订单</th><th>用户</th><th>商品规格</th><th>金额</th><th>状态</th><th>创建时间</th><th></th></tr></thead><tbody><tr v-for="item in orders" :key="item.id"><td><div class="cell-title"><strong>#{{ item.id }}</strong><span class="mono">{{ item.trade_no }}</span></div></td><td><div class="cell-title"><strong>{{ userName(item.user_id) }}</strong><span>用户 #{{ item.user_id }}</span></div></td><td><div class="cell-title"><strong>{{ item.plan_name || `套餐 #${item.plan_id}` }}</strong><span>{{ item.sku_name || `SKU #${item.plan_sku_id}` }}</span></div></td><td>{{ formatCurrency(item.amount_cents, item.currency || 'CNY') }}</td><td><StatusBadge :tone="statusTone(item.status)">{{ statusName(item.status) }}</StatusBadge></td><td>{{ formatDateTime(item.created_at) }}</td><td><div class="cell-actions"><button v-if="item.status === 'pending'" class="button button-ghost button-sm" type="button" @click="requestAction('cancel', item)">取消</button><button v-if="item.status === 'pending' || item.status === 'failed'" class="button button-secondary button-sm" type="button" @click="requestAction('pay', item)">{{ item.status === 'failed' ? '重新确认收款' : '确认收款' }}</button></div></td></tr></tbody></table></div>
      <EmptyState v-else icon="billing" title="没有匹配订单" description="调整状态或用户筛选条件。" />
    </article>

    <ConfirmDialog :open="confirmOpen" :title="actionKind === 'pay' ? '确认订单已经收款？' : '取消这笔订单？'" :message="actionKind === 'pay' ? '确认后将立即进入订阅开通或续费流程，请先核对真实到账记录。' : '取消后用户需要重新创建订单。'" :confirm-text="actionKind === 'pay' ? '确认收款' : '确认取消'" :tone="actionKind === 'cancel' ? 'danger' : 'primary'" :busy="saving" @close="confirmOpen = false" @confirm="executeAction" />
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { cancelOrder, fetchOrders, fetchUsers, markOrderPaid } from '../api/client'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import EmptyState from '../components/EmptyState.vue'
import PageHeader from '../components/PageHeader.vue'
import StatusBadge from '../components/StatusBadge.vue'
import UiIcon from '../components/UiIcon.vue'
import { formatCurrency, formatDateTime } from '../utils/format'

type OrderItem = { id: number; user_id: number; plan_id: number; plan_sku_id: number; plan_name: string; sku_name: string; trade_no: string; status: string; amount_cents: number; currency: string; created_at: string }
type UserItem = { id: number; email: string }
const route = useRoute()
const orders = ref<OrderItem[]>([])
const users = ref<UserItem[]>([])
const statusFilter = ref(String(route.query.status || ''))
const userFilter = ref(String(route.query.user_id || ''))
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const message = ref('')
const confirmOpen = ref(false)
const actionKind = ref<'pay' | 'cancel'>('pay')
const actionTarget = ref<OrderItem | null>(null)
const metrics = computed(() => [
  { label: '待支付', value: orders.value.filter(item => item.status === 'pending').length, icon: 'clock', status: '待处理', tone: 'warning' as const, meta: '需要等待付款或人工核验' },
  { label: '支付失败', value: orders.value.filter(item => item.status === 'failed').length, icon: 'alert', status: '异常', tone: 'danger' as const, meta: '需要核对支付结果' },
  { label: '已支付', value: orders.value.filter(item => item.status === 'paid').length, icon: 'check', status: '已完成', tone: 'success' as const, meta: '已进入订阅交付' },
  { label: '当前列表金额', value: formatCurrency(orders.value.reduce((sum, item) => sum + (item.amount_cents || 0), 0), 'CNY'), icon: 'dollar', status: '列表', tone: 'info' as const, meta: '按当前筛选范围统计' }
])
function userName(id: number) { return users.value.find(user => user.id === id)?.email || `用户 #${id}` }
function statusName(status: string) { return ({ pending: '待支付', paid: '已支付', failed: '失败', canceled: '已取消' } as Record<string, string>)[status] || status }
function statusTone(status: string): 'warning' | 'success' | 'danger' | 'neutral' { return status === 'paid' ? 'success' : status === 'pending' ? 'warning' : status === 'failed' ? 'danger' : 'neutral' }
async function load() { loading.value = true; error.value = ''; try { const [orderData, userData] = await Promise.all([fetchOrders({ status: statusFilter.value || undefined, userId: Number(userFilter.value) || undefined }, true), fetchUsers()]); orders.value = orderData; users.value = userData } catch (e: any) { error.value = e?.response?.data?.message || '订单数据加载失败。' } finally { loading.value = false } }
function requestAction(kind: 'pay' | 'cancel', item: OrderItem) { actionKind.value = kind; actionTarget.value = item; confirmOpen.value = true }
async function executeAction() { if (!actionTarget.value) return; saving.value = true; error.value = ''; message.value = ''; try { if (actionKind.value === 'pay') await markOrderPaid(actionTarget.value.id); else await cancelOrder(actionTarget.value.id, true); message.value = actionKind.value === 'pay' ? `订单 #${actionTarget.value.id} 已确认收款。` : `订单 #${actionTarget.value.id} 已取消。`; confirmOpen.value = false; await load() } catch (e: any) { error.value = e?.response?.data?.message || '订单操作失败。' } finally { saving.value = false } }
onMounted(load)
</script>

<style scoped>
.page-alert,.order-metrics { margin-bottom: 16px; }.count-label { color: var(--muted); font-size: 12px; }.toolbar select { min-width: 170px; }
</style>
