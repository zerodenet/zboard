<template>
  <section class="account-page stack">
    <PageHeader :title="order ? accountOrderName(order) : '订单详情'" :description="order ? `订单 #${order.id}` : ''">
      <template #actions>
        <UiButton as-child variant="secondary"><RouterLink  to="/account/orders">返回我的订单</RouterLink></UiButton>
        <UiButton v-if="orderSupportRoute" as-child variant="secondary"><RouterLink :to="orderSupportRoute">提交工单</RouterLink></UiButton>
        <PageRefreshButton label="刷新订单" :loading="loading" @click="load" />
      </template>
    </PageHeader>
    <PageAlert v-if="error" tone="danger">{{ error }}</PageAlert>
    <TableSkeleton v-if="loading && !order" label="正在加载订单" :columns="2" :rows="3" />
    <div v-if="order" class="order-detail-layout">
      <UiSection title="订单信息" class="order-detail-info" variant="outlined">
        <dl class="order-detail-fields">

          <div v-if="order.sku_name && order.order_type !== 'topup'"><dt>规格</dt><dd>{{ order.sku_name }}</dd></div>


          <div><dt>创建时间</dt><dd><TimeBadge :value="order.created_at" /></dd></div>

        </dl>
        <details class="order-detail-more"><summary>更多订单信息</summary><dl class="order-detail-fields"><div><dt>交易编号</dt><dd>{{ order.trade_no || '—' }}</dd></div><div><dt>更新时间</dt><dd><TimeBadge :value="order.updated_at" /></dd></div></dl></details>
        <PageAlert v-if="order.subscription_ended_at" tone="info">
          原订阅{{ order.subscription_end_reason === 'exhausted' ? '已用完' : '已到期' }} · <TimeBadge :value="order.subscription_ended_at" />
          <br>最终用量 {{ formatBytes(order.subscription_final_flow_used || 0) }} / {{ formatBytes(order.subscription_final_flow_total || 0) }}
        </PageAlert>
      </UiSection>
      <UiSection title="付款状态" class="order-detail-payment-section" variant="outlined">
        <div class="order-detail-payment">
          <p class="order-detail-amount">{{ formatCurrency(order.payable_amount ?? order.amount_cents, order.currency) }}</p>
          <StatusBadge :tone="order.status === 'paid' ? 'success' : order.status === 'pending' ? 'warning' : order.status === 'failed' ? 'danger' : 'neutral'">{{ statusLabel }}</StatusBadge>
          <RouterLink v-if="order.status === 'paid' && order.order_type === 'topup'" :to="{ path: '/account/wallet', query: { currency: order.currency } }">查看余额与流水</RouterLink>
          <OrderFundingPayment v-if="payable && !error && order.order_type !== 'topup'" :order-id="order.id" :currency="order.currency" :preferred-plugin="returnPlugin" @changed="load" />
          <CheckoutPayment v-if="payable && !error && order.order_type === 'topup'" :key="order.id" name="account.orders.payment" embedded :order-id="order.id" :currency="order.currency" :preferred-plugin="returnPlugin" :reconcile-on-load="Boolean(returnPlugin)" :open-on-load="Boolean(returnPlugin)" @paid="load" />
          <p v-if="payable" class="order-detail-hint">付款结果以本站核对后的订单状态为准。</p>
        </div>
      </UiSection>
    </div>
    <PaymentReceipts v-if="order" :order-id="order.id" @changed="load" />
  </section>
</template>

<script setup lang="ts">
import { computed, watch } from 'vue'
import { useRoute } from 'vue-router'
import { fetchAccountOrder, type AdminOrderListItem } from '../../api/client'
import { useRemoteResource } from '../../composables/useRemoteResource'
import { accountOrderName } from '../../utils/accountOrders'
import PageRefreshButton from '../../components/PageRefreshButton.vue'
import TableSkeleton from '../../components/TableSkeleton.vue'
import PageHeader from '../../components/PageHeader.vue'
import PageAlert from '../../components/PageAlert.vue'
import UiSection from '../../components/UiSection.vue'
import UiButton from '../../components/UiButton.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import TimeBadge from '../../components/TimeBadge.vue'
import CheckoutPayment from '../../plugins/CheckoutPayment.vue'
import OrderFundingPayment from '../../components/OrderFundingPayment.vue'
import PaymentReceipts from '../../components/PaymentReceipts.vue'
import { formatBytes, formatCurrency, formatUnknownValue } from '../../utils/format'

const route = useRoute()
const orderId = computed(() => Number(route.params.id))
const returnPlugin = computed(() => typeof route.query.payment_plugin === 'string' ? route.query.payment_plugin : '')
const { data: order, loading, error, load, reset } = useRemoteResource<AdminOrderListItem | null>({
  initial: () => null,
  fetch: ({ signal }) => {
    if (!Number.isSafeInteger(orderId.value) || orderId.value <= 0) throw new Error('订单编号无效。')
    return fetchAccountOrder(orderId.value, { signal })
  },
  errorMessage: '订单不存在、当前账户无权查看或暂时无法加载，请重试。',
})
const payable = computed(() => order.value && ['pending', 'failed'].includes(order.value.status))
const orderSupportRoute = computed(() => order.value && !error.value && Number.isSafeInteger(order.value.id) && order.value.id > 0
  ? { path: '/account/tickets', query: { new: '1', order_id: String(order.value.id) } } : null)
const statusLabel = computed(() => {
  const status = order.value?.status || ''
  return ({ pending: '待支付', paid: '已支付', failed: '失败', canceled: '已取消' } as Record<string, string>)[status] || formatUnknownValue('状态', status)
})
watch(orderId, () => { reset(); void load() }, { immediate: true })
</script>

<style scoped>
.order-detail-layout { display: grid; grid-template-columns: minmax(0, 1fr) minmax(17rem, 22rem); gap: 20px; align-items: start; }
.order-detail-fields { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 20px; margin: 0; padding: 0; }
.order-detail-fields div, .order-detail-payment { display: grid; gap: 8px; min-width: 0; }
.order-detail-fields dt, .order-detail-hint { color: var(--muted); font-size: 12px; }
.order-detail-fields dd { margin: 0; overflow-wrap: anywhere; }
.order-detail-payment { justify-items: start; padding: 0; }
.order-detail-payment > .checkout-payment { width: 100%; }
.order-detail-payment p { margin: 0; }
.order-detail-amount { font-size: 30px; font-weight: 600; }
.order-detail-more { margin: 16px 0 0; font-size: 12px; }
.order-detail-more summary { cursor: pointer; color: var(--muted-foreground); }
.order-detail-more .order-detail-fields { padding: 16px 0 0; }
@media (max-width: 760px) { .order-detail-layout, .order-detail-fields { grid-template-columns: 1fr; } .order-detail-payment-section { grid-row: 1; } }
</style>
