<template>
  <section class="standard-page">
    <PageHeader title="订单管理" description="处理全站交易状态和人工收款，不混入管理员自己的购买与订阅操作。" eyebrow="Commerce">
      <template #actions><PageRefreshButton label="刷新订单" :loading="loading" @click="load" /></template>
    </PageHeader>

    <TransientFeedback :success="message" :error="error" success-title="订单状态已更新" error-title="订单操作失败" />

    <DataWorkbench :total="total" :loading="loading" :refreshing="refreshing">
      <template #filters>
        <WorkbenchFilterBar :active="hasFilters" @clear="clearFilters">
          <WorkbenchFilterInput v-model="queryFilter" label="搜索" maxlength="128" placeholder="订单号、交易号、套餐或渠道" @apply="applyFilters" />
          <WorkbenchFilterSelect v-model="statusFilter" label="订单状态" :options="statusOptions" @apply="applyFilters" />
          <WorkbenchFilterSelect v-model="orderTypeFilter" label="订单类型" :options="orderTypeOptions" @apply="applyFilters" />
          <WorkbenchFilterInput v-model="userFilter" label="用户 ID" value-prefix="#" inputmode="numeric" @apply="applyFilters" />
          <WorkbenchFilterDate v-model:from="createdFrom" v-model:to="createdTo" label="创建日期" @apply="applyFilters" />
        </WorkbenchFilterBar>
      </template>
      <template #actions><RouterLink class="button button-ghost button-sm" :to="adminContextLink('/admin/plans')">管理商品<UiIcon name="chevron" /></RouterLink></template>
      <DataTable v-if="orders.length" caption="订单管理列表" :row-count="total" :min-width="980"><thead><tr><th class="table-primary-column">订单</th><th data-column-priority="3">用户</th><th data-column-priority="2">商品规格</th><th data-column-priority="1">金额</th><th>状态</th><th data-column-priority="2">创建时间</th><th class="table-action-column"><span class="sr-only">操作</span></th></tr></thead><tbody><tr v-for="item in orders" :key="item.id"><td class="table-primary-column"><div class="cell-title"><strong>#{{ item.id }}</strong><span class="mono">{{ item.trade_no }}</span></div></td><td class="mono" data-column-priority="3">#{{ item.user_id }}</td><td data-column-priority="2"><div class="cell-title"><strong>{{ item.plan_name || `套餐 #${item.plan_id}` }}</strong><span>{{ item.sku_name || `SKU #${item.plan_sku_id}` }}</span></div></td><td data-column-priority="1">{{ formatCurrency(item.amount_cents, item.currency || 'CNY') }}</td><td><StatusBadge :tone="statusTone(item.status)">{{ statusName(item.status) }}</StatusBadge></td><td data-column-priority="2"><TimeBadge :value="item.created_at" /></td><td class="table-action-column"><RowActions :label="`订单 #${item.id} 的操作`" :trigger-key="`order-${item.id}`"><UiButton variant="secondary" size="sm" type="button" :data-order-detail-trigger="item.id" @click="openDetail(item.id)">查看详情</UiButton><RouterLink class="button button-ghost button-sm" :to="adminContextLink('/admin/subscriptions', { user_id: String(item.user_id) })">用户订阅</RouterLink><RouterLink class="button button-ghost button-sm" :to="adminContextLink('/admin/users', { user: String(item.user_id) })">用户详情</RouterLink><UiButton v-if="item.status === 'pending'" variant="ghost" size="sm" type="button" @click="requestAction('cancel', item)">取消订单</UiButton><UiButton v-if="item.status === 'pending' || item.status === 'failed'" variant="ghost" size="sm" type="button" @click="requestAction('pay', item)">{{ item.status === 'failed' ? '重新确认收款' : '确认收款' }}</UiButton></RowActions></td></tr></tbody></DataTable>
      <EmptyState v-else icon="billing" title="没有匹配订单" description="调整状态或用户筛选条件。" />
      <template #footer><TablePager :total="total" :offset="offset" :limit="limit" :loading="loading" @change="changePage" /></template>
    </DataWorkbench>

    <DetailDrawer :open="Boolean(detailID)" :title="selectedOrder ? `订单 #${selectedOrder.id}` : '订单详情'" eyebrow="Order" :description="selectedOrder?.trade_no || '正在加载交易与履约快照'" :return-focus-selector="detailID ? `[data-row-action-trigger='order-${detailID}']` : ''" @close="closeDetail">
      <PageAlert v-if="detailError" tone="danger" title="订单详情加载失败">{{ detailError }}</PageAlert>
      <div v-if="detailLoading" class="detail-loading" role="status">正在加载订单详情…</div>
      <main v-else-if="selectedOrder" class="business-detail stack">
        <section class="detail-status-strip" aria-label="订单状态">
          <StatusBadge :tone="statusTone(selectedOrder.status)">{{ statusName(selectedOrder.status) }}</StatusBadge>
          <StatusBadge tone="info">{{ orderTypeName(selectedOrder.order_type) }}</StatusBadge>
          <StatusBadge :tone="selectedOrder.fulfilled_at ? 'success' : 'neutral'">{{ selectedOrder.fulfilled_at ? '已履约' : '未履约' }}</StatusBadge>
        </section>
        <PageAlert v-if="selectedOrder.failure_reason" tone="danger" title="最近失败原因">{{ safeFailureReason(selectedOrder.failure_reason) }}</PageAlert>
        <section class="detail-metrics" aria-label="订单金额">
          <div><strong>{{ formatCurrency(selectedOrder.amount_cents, selectedOrder.currency || 'CNY') }}</strong><span>订单金额</span></div>
          <div><strong>{{ formatCurrency(selectedOrder.paid_amount, selectedOrder.currency || 'CNY') }}</strong><span>实收金额</span></div>
          <div><strong>{{ formatCurrency(selectedOrder.discount_amount, selectedOrder.currency || 'CNY') }}</strong><span>优惠金额</span></div>
          <div><strong>{{ formatCurrency(selectedOrder.refund_amount, selectedOrder.currency || 'CNY') }}</strong><span>退款金额</span></div>
        </section>
        <section class="detail-facts" aria-label="交易与权益快照">
          <div><span>商品规格</span><strong>{{ selectedOrder.plan_name || `套餐 #${selectedOrder.plan_id}` }} / {{ selectedOrder.sku_name || `SKU #${selectedOrder.plan_sku_id}` }}</strong></div>
          <div><span>用户</span><strong class="mono">#{{ selectedOrder.user_id }}</strong></div>
          <div><span>支付渠道</span><strong>{{ selectedOrder.channel || '未指定' }}</strong></div>
          <div><span>渠道交易号</span><strong class="mono">{{ selectedOrder.provider_trade_no || '未生成' }}</strong></div>
          <div><span>关联订阅</span><strong class="mono">{{ selectedOrder.subscription_id ? `#${selectedOrder.subscription_id}` : '尚未创建' }}</strong></div>
          <div><span>目标订阅</span><strong class="mono">{{ selectedOrder.target_subscription_id ? `#${selectedOrder.target_subscription_id}` : '无' }}</strong></div>
          <div><span>计费周期</span><strong>{{ billingName(selectedOrder.billing_unit, selectedOrder.billing_value) }}</strong></div>
          <div><span>流量权益</span><strong>{{ formatBytes(selectedOrder.traffic_bytes) }}</strong></div>
          <div><span>设备数</span><strong>{{ selectedOrder.device_limit }}</strong></div>
          <div><span>速率</span><strong>{{ selectedOrder.speed_limit_mbps ? `${selectedOrder.speed_limit_mbps} Mbps` : '不限速' }}</strong></div>
          <div><span>创建时间</span><TimeBadge :value="selectedOrder.created_at" /></div>
          <div><span>支付时间</span><TimeBadge :value="selectedOrder.paid_at" /></div>
          <div><span>履约时间</span><TimeBadge :value="selectedOrder.fulfilled_at" /></div>
          <div><span>更新时间</span><TimeBadge :value="selectedOrder.updated_at" /></div>
        </section>
        <section class="payment-events stack" aria-label="支付事件时间线">
          <header class="detail-section-header"><div><h3>支付事件</h3><p>仅展示规范化事件摘要；回调原文不会进入前端。</p></div><span>{{ paymentEventTotal }}</span></header>
          <PageAlert v-if="paymentEventError" tone="danger" title="支付事件加载失败">{{ paymentEventError }}</PageAlert>
          <DataTable v-if="paymentEvents.length" caption="订单支付事件安全摘要" :row-count="paymentEventTotal" :min-width="760">
            <thead><tr><th class="table-primary-column">事件</th><th>渠道</th><th data-column-priority="1">金额</th><th>验签</th><th data-column-priority="2">处理时间</th><th data-column-priority="2">接收时间</th></tr></thead>
            <tbody><tr v-for="event in paymentEvents" :key="event.id"><td class="table-primary-column"><div class="cell-title"><strong>{{ eventTypeName(event.event_type) }}</strong><span class="mono">{{ safeEventReference(event.provider_event_id) }}</span></div></td><td>{{ providerName(event.provider) }}</td><td data-column-priority="1">{{ formatCurrency(event.amount_minor, selectedOrder.currency || 'CNY') }}</td><td><StatusBadge :tone="event.signature_valid ? 'success' : 'danger'">{{ event.signature_valid ? '验签通过' : '验签失败' }}</StatusBadge></td><td data-column-priority="2"><TimeBadge :value="event.processed_at" /></td><td data-column-priority="2"><TimeBadge :value="event.created_at" /></td></tr></tbody>
          </DataTable>
          <EmptyState v-else-if="!paymentEventLoading && !paymentEventError" icon="billing" title="暂无支付事件" description="人工订单或尚未收到渠道回调时可能没有事件记录。" />
          <div v-if="paymentEventLoading" class="detail-loading" role="status">正在加载支付事件…</div>
          <TablePager v-if="paymentEventTotal > paymentEventLimit" :total="paymentEventTotal" :offset="paymentEventOffset" :limit="paymentEventLimit" :loading="paymentEventLoading" @change="changePaymentEventPage" />
        </section>
        <div class="detail-action-row">
          <RouterLink class="button button-ghost button-sm" :to="adminContextLink('/admin/subscriptions', { user_id: String(selectedOrder.user_id), ...(selectedOrder.subscription_id ? { subscription: String(selectedOrder.subscription_id) } : {}) })">查看用户订阅</RouterLink>
          <RouterLink class="button button-ghost button-sm" :to="adminContextLink('/admin/users', { user: String(selectedOrder.user_id) })">查看用户详情</RouterLink>
          <UiButton v-if="selectedOrder.status === 'pending'" variant="danger" size="sm" type="button" @click="requestAction('cancel', selectedOrder)">取消订单</UiButton>
          <UiButton v-if="selectedOrder.status === 'pending' || selectedOrder.status === 'failed'" size="sm" type="button" @click="requestAction('pay', selectedOrder)">{{ selectedOrder.status === 'failed' ? '重新确认收款' : '确认收款' }}</UiButton>
        </div>
      </main>
    </DetailDrawer>

    <ConfirmDialog :open="confirmOpen" :title="actionKind === 'pay' ? '确认订单已经收款？' : '取消这笔订单？'" :message="actionKind === 'pay' ? '确认后将立即进入订阅开通或续费流程，请先核对真实到账记录。' : '取消后用户需要重新创建订单。'" :confirm-text="actionKind === 'pay' ? '确认收款' : '确认取消'" :tone="actionKind === 'cancel' ? 'danger' : 'primary'" :busy="saving" @close="confirmOpen = false" @confirm="executeAction" />
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { cancelOrder, fetchAdminOrderDetail, fetchAdminOrderPaymentEvents, fetchOrdersPage, markOrderPaid, type AdminOrderDetail, type AdminOrderListItem, type AdminPaymentEventSummary } from '../api/client'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import DataTable from '../components/DataTable.vue'
import DataWorkbench from '../components/DataWorkbench.vue'
import DetailDrawer from '../components/DetailDrawer.vue'
import EmptyState from '../components/EmptyState.vue'
import PageAlert from '../components/PageAlert.vue'
import PageHeader from '../components/PageHeader.vue'
import RowActions from '../components/RowActions.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TablePager from '../components/TablePager.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import UiIcon from '../components/UiIcon.vue'
import WorkbenchFilterBar from '../components/WorkbenchFilterBar.vue'
import WorkbenchFilterDate from '../components/WorkbenchFilterDate.vue'
import WorkbenchFilterInput from '../components/WorkbenchFilterInput.vue'
import WorkbenchFilterSelect from '../components/WorkbenchFilterSelect.vue'
import { useRemoteTable } from '../composables/useRemoteTable'
import { formatBytes, formatCurrency, formatUnknownValue } from '../utils/format'
import { normalizeOutput, truncateOutput } from '../utils/output'
import { preserveAdminReturnTo, withAdminReturnTo } from '../utils/navigation'

type OrderItem = AdminOrderListItem
const route = useRoute()
const router = useRouter()
const queryFilter = ref(String(route.query.q || ''))
const statusFilter = ref(String(route.query.status || ''))
const orderTypeFilter = ref(String(route.query.order_type || ''))
const userFilter = ref(String(route.query.user_id || ''))
const createdFrom = ref(String(route.query.created_from || ''))
const createdTo = ref(String(route.query.created_to || ''))
const allowedPageSizes = [25, 50, 100]
const initialLimit = Number(route.query.limit)
const limit = ref(allowedPageSizes.includes(initialLimit) ? initialLimit : 50)
const offset = ref((Math.max(1, Number(route.query.page) || 1) - 1) * limit.value)
const saving = ref(false)
const message = ref('')
const confirmOpen = ref(false)
const actionKind = ref<'pay' | 'cancel'>('pay')
const actionTarget = ref<OrderItem | null>(null)
const selectedOrder = ref<AdminOrderDetail | null>(null)
const detailID = ref(0)
const detailLoading = ref(false)
const detailError = ref('')
let detailController: AbortController | null = null
const paymentEventOffset = ref(0)
const paymentEventLimit = ref(25)
const statusOptions = [
  { label: '全部状态', value: '' },
  { label: '待处理（待支付 / 失败）', value: 'attention' },
  { label: '待支付', value: 'pending' },
  { label: '已支付', value: 'paid' },
  { label: '失败', value: 'failed' },
  { label: '已取消', value: 'canceled' },
]
const orderTypeOptions = [
  { label: '全部类型', value: '' },
  { label: '新购', value: 'new' },
  { label: '续费', value: 'renewal' },
  { label: '升级', value: 'upgrade' },
  { label: '流量包', value: 'traffic_pack' },
]
const hasFilters = computed(() => Boolean(queryFilter.value || statusFilter.value || orderTypeFilter.value || userFilter.value || createdFrom.value || createdTo.value))
const { items: orders, total, loading, refreshing, error, load } = useRemoteTable<OrderItem>({
  offset,
  limit,
  fetchPage: ({ signal }) => fetchOrdersPage({ q: queryFilter.value || undefined, status: statusFilter.value || undefined, orderType: orderTypeFilter.value || undefined, userId: Number(userFilter.value) || undefined, createdFrom: createdFrom.value || undefined, createdTo: createdTo.value || undefined, offset: offset.value, limit: limit.value }, { signal }),
  errorMessage: (cause: any) => cause?.response?.data?.message || '订单数据加载失败。',
  onOffsetCorrected: () => syncURL(true),
})
const { items: paymentEvents, total: paymentEventTotal, loading: paymentEventLoading, error: paymentEventError, load: loadPaymentEvents } = useRemoteTable<AdminPaymentEventSummary>({
  offset: paymentEventOffset,
  limit: paymentEventLimit,
  fetchPage: ({ signal }) => detailID.value
    ? fetchAdminOrderPaymentEvents(detailID.value, { offset: paymentEventOffset.value, limit: paymentEventLimit.value }, { signal })
    : Promise.resolve({ items: [], total: 0 }),
  errorMessage: (cause: any) => cause?.response?.data?.message || '支付事件加载失败。',
  onOffsetCorrected: () => syncURL(true),
})
function statusName(status: string) { return ({ pending: '待支付', paid: '已支付', failed: '失败', canceled: '已取消' } as Record<string, string>)[status] || formatUnknownValue('状态', status) }
function statusTone(status: string): 'warning' | 'success' | 'danger' | 'neutral' { return status === 'paid' ? 'success' : status === 'pending' ? 'warning' : status === 'failed' ? 'danger' : 'neutral' }
function orderTypeName(value: string) { return ({ new: '新购', renewal: '续费', upgrade: '升级', traffic_pack: '流量包' } as Record<string, string>)[value] || formatUnknownValue('订单类型', value) }
function billingName(unit: string, value: number) { const label = ({ day: '天', month: '个月', year: '年' } as Record<string, string>)[unit]; return label ? `${value || 0} ${label}` : formatUnknownValue('计费周期', `${value || 0} ${unit || ''}`.trim()) }
function safeFailureReason(value: string) { return truncateOutput(normalizeOutput(value), 360) || '未提供失败原因。' }
function safeEventReference(value: string) { return truncateOutput(normalizeOutput(value), 96) || '无渠道事件号' }
function providerName(value: string) { return truncateOutput(normalizeOutput(value), 32) || '未知渠道' }
function eventTypeName(value: string) { return truncateOutput(normalizeOutput(value).replace(/[._-]+/g, ' '), 48) || '未知事件' }
function adminContextLink(path: string, query: Record<string, string> = {}) { return withAdminReturnTo(path, route.fullPath, query) }
async function syncURL(replace = false) { const page = Math.floor(offset.value / limit.value) + 1, eventPage = Math.floor(paymentEventOffset.value / paymentEventLimit.value) + 1; const location = { query: { ...preserveAdminReturnTo(route.query.return_to), ...(queryFilter.value ? { q: queryFilter.value } : {}), ...(statusFilter.value ? { status: statusFilter.value } : {}), ...(orderTypeFilter.value ? { order_type: orderTypeFilter.value } : {}), ...(userFilter.value ? { user_id: userFilter.value } : {}), ...(createdFrom.value ? { created_from: createdFrom.value } : {}), ...(createdTo.value ? { created_to: createdTo.value } : {}), ...(page > 1 ? { page: String(page) } : {}), ...(limit.value !== 50 ? { limit: String(limit.value) } : {}), ...(detailID.value ? { order: String(detailID.value) } : {}), ...(detailID.value && eventPage > 1 ? { event_page: String(eventPage) } : {}), ...(detailID.value && paymentEventLimit.value !== 25 ? { event_limit: String(paymentEventLimit.value) } : {}) } }; await (replace ? router.replace(location) : router.push(location)) }
async function applyFilters() { offset.value = 0; await syncURL(); await load() }
async function clearFilters() { queryFilter.value = ''; statusFilter.value = ''; orderTypeFilter.value = ''; userFilter.value = ''; createdFrom.value = ''; createdTo.value = ''; offset.value = 0; await syncURL(); await load() }
async function changePage(value: { offset: number; limit: number }) { offset.value = value.offset; limit.value = value.limit; await syncURL(); await load() }
function requestAction(kind: 'pay' | 'cancel', item: OrderItem) { actionKind.value = kind; actionTarget.value = item; confirmOpen.value = true }
async function executeAction() { if (!actionTarget.value) return; saving.value = true; error.value = ''; message.value = ''; try { if (actionKind.value === 'pay') await markOrderPaid(actionTarget.value.id); else await cancelOrder(actionTarget.value.id, true); message.value = actionKind.value === 'pay' ? `订单 #${actionTarget.value.id} 已确认收款。` : `订单 #${actionTarget.value.id} 已取消。`; confirmOpen.value = false; await load(); if (detailID.value === actionTarget.value.id) { selectedOrder.value = null; await syncDetailFromRoute() } } catch (e: any) { error.value = e?.response?.data?.message || '订单操作失败。' } finally { saving.value = false } }
async function openDetail(id: number) { const { event_page: _eventPage, event_limit: _eventLimit, ...query } = route.query; paymentEventOffset.value = 0; paymentEventLimit.value = 25; await router.push({ query: { ...query, order: String(id) } }) }
async function closeDetail() { detailController?.abort(); detailID.value = 0; selectedOrder.value = null; detailError.value = ''; paymentEvents.value = []; const { order: _order, event_page: _eventPage, event_limit: _eventLimit, ...query } = route.query; await router.push({ query }) }
async function changePaymentEventPage(value: { offset: number; limit: number }) { paymentEventOffset.value = value.offset; paymentEventLimit.value = value.limit; await syncURL(); await loadPaymentEvents() }
async function syncDetailFromRoute() {
  const id = Number(route.query.order)
  if (!Number.isInteger(id) || id <= 0) {
    detailController?.abort(); detailID.value = 0; selectedOrder.value = null; detailError.value = ''; detailLoading.value = false; paymentEvents.value = []
    return
  }
  const rawEventLimit = Number(route.query.event_limit)
  const nextEventLimit = allowedPageSizes.includes(rawEventLimit) ? rawEventLimit : 25
  const nextEventOffset = (Math.max(1, Number(route.query.event_page) || 1) - 1) * nextEventLimit
  const eventPageChanged = nextEventLimit !== paymentEventLimit.value || nextEventOffset !== paymentEventOffset.value
  paymentEventLimit.value = nextEventLimit
  paymentEventOffset.value = nextEventOffset
  if (detailID.value === id && (selectedOrder.value?.id === id || detailLoading.value)) {
    if (eventPageChanged && !detailLoading.value) await loadPaymentEvents()
    return
  }
  detailController?.abort()
  detailController = new AbortController()
  detailID.value = id; selectedOrder.value = null; detailError.value = ''; detailLoading.value = true
  try {
    const eventLoad = loadPaymentEvents()
    selectedOrder.value = await fetchAdminOrderDetail(id, { signal: detailController.signal })
    await eventLoad
  }
  catch (cause: any) { if (cause?.name !== 'CanceledError' && cause?.name !== 'AbortError') detailError.value = cause?.response?.data?.message || '订单详情加载失败。' }
  finally { if (detailID.value === id) detailLoading.value = false }
}
watch(() => route.fullPath, async () => { const nextQuery = String(route.query.q || ''), nextStatus = String(route.query.status || ''), nextOrderType = String(route.query.order_type || ''), nextUser = String(route.query.user_id || ''), nextCreatedFrom = String(route.query.created_from || ''), nextCreatedTo = String(route.query.created_to || ''); const rawLimit = Number(route.query.limit), nextLimit = allowedPageSizes.includes(rawLimit) ? rawLimit : 50, nextOffset = (Math.max(1, Number(route.query.page) || 1) - 1) * nextLimit; if (nextQuery !== queryFilter.value || nextStatus !== statusFilter.value || nextOrderType !== orderTypeFilter.value || nextUser !== userFilter.value || nextCreatedFrom !== createdFrom.value || nextCreatedTo !== createdTo.value || nextLimit !== limit.value || nextOffset !== offset.value) { queryFilter.value = nextQuery; statusFilter.value = nextStatus; orderTypeFilter.value = nextOrderType; userFilter.value = nextUser; createdFrom.value = nextCreatedFrom; createdTo.value = nextCreatedTo; limit.value = nextLimit; offset.value = nextOffset; await load() } await syncDetailFromRoute() })
onMounted(async () => { await load(); await syncDetailFromRoute() })
</script>

<style scoped>
.page-alert,.order-metrics { margin-bottom: 16px; }.count-label { color: var(--muted); font-size: 12px; }.toolbar select { min-width: 170px; }
.toolbar .p-select { min-width: 170px; }
</style>
