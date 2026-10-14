<template>
  <section class="account-page stack">
    <PageHeader :title="order ? `订单 #${order.id}` : '订单详情'" description="查看这笔订单的商品、金额和付款状态。" eyebrow="ORDER">
      <template #actions>
        <RouterLink class="button button-secondary" to="/account/orders">返回我的订单</RouterLink>
        <UiButton variant="secondary" type="button" :disabled="loading" @click="load">刷新订单</UiButton>
      </template>
    </PageHeader>
    <PageAlert v-if="error" tone="danger">{{ error }}</PageAlert>
    <p v-if="loading && !order" role="status">正在加载订单…</p>
    <div v-if="order" class="order-detail-layout">
      <UiSection title="订单信息" variant="outlined">
        <dl class="order-detail-fields">
          <div><dt>商品</dt><dd>{{ order.plan_name || `套餐 #${order.plan_id}` }}</dd></div>
          <div><dt>规格</dt><dd>{{ order.sku_name || `SKU #${order.plan_sku_id}` }}</dd></div>
          <div><dt>订单编号</dt><dd>#{{ order.id }}</dd></div>
          <div><dt>交易编号</dt><dd>{{ order.trade_no }}</dd></div>
          <div><dt>创建时间</dt><dd><TimeBadge :value="order.created_at" /></dd></div>
          <div><dt>更新时间</dt><dd><TimeBadge :value="order.updated_at" /></dd></div>
        </dl>
        <PageAlert v-if="order.subscription_ended_at" tone="info">
          原订阅{{ order.subscription_end_reason === 'exhausted' ? '已用完' : '已到期' }} · <TimeBadge :value="order.subscription_ended_at" />
          <br>最终用量 {{ formatBytes(order.subscription_final_flow_used || 0) }} / {{ formatBytes(order.subscription_final_flow_total || 0) }}
        </PageAlert>
      </UiSection>
      <UiSection title="付款状态" variant="outlined">
        <div class="order-detail-payment">
          <p class="order-detail-amount">{{ formatCurrency(order.payable_amount ?? order.amount_cents, order.currency) }}</p>
          <StatusBadge :tone="order.status === 'paid' ? 'success' : order.status === 'pending' ? 'warning' : order.status === 'failed' ? 'danger' : 'neutral'">{{ statusLabel }}</StatusBadge>
          <p v-if="order.status === 'paid'">本站已确认这笔订单付款完成。</p>
          <p v-else-if="order.status === 'canceled'">此订单已取消，不可继续支付。</p>
          <CheckoutPayment v-if="payable && !error" :key="order.id" name="account.orders.payment" embedded :order-id="order.id" :currency="order.currency" :preferred-plugin="returnPlugin" :reconcile-on-load="Boolean(returnPlugin)" :open-on-load="Boolean(returnPlugin)" @paid="load" />
          <p v-if="payable" class="order-detail-hint">付款结果以本站核对后的订单状态为准。</p>
        </div>
      </UiSection>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, watch } from 'vue'
import { useRoute } from 'vue-router'
import { fetchAccountOrder, type AdminOrderListItem } from '../../api/client'
import { useRemoteResource } from '../../composables/useRemoteResource'
import PageHeader from '../../components/PageHeader.vue'
import PageAlert from '../../components/PageAlert.vue'
import UiSection from '../../components/UiSection.vue'
import UiButton from '../../components/UiButton.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import TimeBadge from '../../components/TimeBadge.vue'
import CheckoutPayment from '../../plugins/CheckoutPayment.vue'
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
const statusLabel = computed(() => {
  const status = order.value?.status || ''
  return ({ pending: '待支付', paid: '已支付', failed: '失败', canceled: '已取消' } as Record<string, string>)[status] || formatUnknownValue('状态', status)
})
watch(orderId, () => { reset(); void load() }, { immediate: true })
</script>

<style scoped>
.order-detail-layout { display: grid; grid-template-columns: minmax(0, 1fr) minmax(17rem, 22rem); gap: 20px; align-items: start; }
.order-detail-fields { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 20px; margin: 0; padding: 20px; }
.order-detail-fields div, .order-detail-payment { display: grid; gap: 8px; min-width: 0; }
.order-detail-fields dt, .order-detail-hint { color: var(--muted); font-size: 12px; }
.order-detail-fields dd { margin: 0; overflow-wrap: anywhere; }
.order-detail-payment { justify-items: start; padding: 20px; }
.order-detail-payment > .checkout-payment { width: 100%; }
.order-detail-payment p { margin: 0; }
.order-detail-amount { font-size: 30px; font-weight: 700; }
@media (max-width: 760px) { .order-detail-layout, .order-detail-fields { grid-template-columns: 1fr; } }
</style>
