<template>
  <ul class="account-order-list" :class="{ 'with-actions': Boolean($slots.actions) }" aria-label="订单记录">
    <li v-for="order in orders" :key="order.id">
      <div class="account-order-row">
        <RouterLink class="account-order-link" :to="`/account/orders/${order.id}`">
          <span class="account-order-name"><strong>{{ accountOrderName(order) }}</strong><small><span>#{{ order.id }}</span><span v-if="order.sku_name && order.order_type !== 'topup'">{{ order.sku_name }}</span></small></span>
          <StatusBadge :tone="tone(order.status)">{{ label(order.status) }}</StatusBadge>
          <strong class="account-order-amount">{{ formatCurrency(order.payable_amount ?? order.amount_cents, order.currency) }}</strong>
          <TimeBadge class="account-order-time" :value="order.created_at" mode="relative" />
          <UiIcon class="account-order-chevron" name="chevron" />
        </RouterLink>
        <div v-if="$slots.actions" class="account-order-actions"><slot name="actions" :order="order" /></div>
      </div>
      <div v-if="order.subscription_ended_at" class="account-order-history">
        <span>原订阅{{ order.subscription_end_reason === 'exhausted' ? '已用完' : '已到期' }} · <TimeBadge :value="order.subscription_ended_at" /></span>
        <span>最终用量 {{ formatBytes(order.subscription_final_flow_used || 0) }} / {{ formatBytes(order.subscription_final_flow_total || 0) }}</span>
      </div>
    </li>
  </ul>
</template>
<script setup lang="ts">
import type { AdminOrderListItem } from '../../api/client'
import { accountOrderName } from '../../utils/accountOrders'
import { formatBytes, formatCurrency, formatUnknownValue } from '../../utils/format'
import StatusBadge from '../StatusBadge.vue'
import TimeBadge from '../TimeBadge.vue'
import UiIcon from '../UiIcon.vue'
defineProps<{ orders: AdminOrderListItem[] }>()
function tone(status: string): 'success' | 'warning' | 'danger' | 'neutral' { return status === 'paid' ? 'success' : status === 'pending' ? 'warning' : status === 'failed' ? 'danger' : 'neutral' }
function label(status: string) { return ({ pending: '待支付', paid: '已支付', failed: '失败', canceled: '已取消' } as Record<string, string>)[status] || formatUnknownValue('状态', status) }
</script>
<style scoped>
.account-order-list { margin: 0; padding: 0; list-style: none; display: grid; gap: 0; }
.account-order-list > li { min-width: 0; }
.account-order-list > li + li { border-top: 1px solid var(--line-subtle); }
.account-order-row { display: flex; align-items: center; gap: 12px; }
.account-order-list.with-actions .account-order-row { display: grid; grid-template-columns: minmax(0, 1fr) 180px; }
.account-order-link { flex: 1; min-width: 0; display: grid; grid-template-columns: minmax(0, 1fr) 80px 100px 100px 16px; align-items: center; gap: 16px; min-height: 56px; padding: 10px 8px; color: var(--foreground); text-decoration: none; }
.account-order-link:hover { background: var(--muted-surface); }
.account-order-link:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px; border-radius: 4px; }
.account-order-name { min-width: 0; display: grid; gap: 6px; }
.account-order-name strong { overflow-wrap: anywhere; font-size: 13px; font-weight: 500; }
.account-order-name small { display: flex; flex-wrap: wrap; gap: 8px; color: var(--muted-foreground); font-size: 12px; }
.account-order-amount { text-align: right; font-size: 13px; font-weight: 500; font-variant-numeric: tabular-nums; }
.account-order-time { justify-self: end; }
.account-order-chevron { width: 14px; color: var(--muted-foreground); }
.account-order-actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 8px; }
.account-order-history { display: flex; flex-wrap: wrap; gap: 8px 20px; padding: 0 8px 14px; color: var(--muted-foreground); font-size: 12px; }
@media (max-width: 760px) {
  .account-order-list.with-actions .account-order-row { display: flex; }
  .account-order-row { align-items: stretch; flex-direction: column; gap: 0; }
  .account-order-link { grid-template-columns: minmax(0, 1fr) auto; gap: 10px 16px; padding: 16px 0; }
  .account-order-name { grid-column: 1; grid-row: 1 / 3; }
  .account-order-amount { grid-column: 2; grid-row: 1; }
  .account-order-link > .status-badge { grid-column: 2; grid-row: 2; justify-self: end; }
  .account-order-time, .account-order-chevron { display: none; }
  .account-order-actions:not(:empty) { padding-bottom: 14px; }
  .account-order-actions :deep(.ui-button) { min-height: 40px; }
}
</style>
