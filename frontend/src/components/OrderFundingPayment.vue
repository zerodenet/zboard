<template>
  <section class="stack" aria-label="订单付款分配">
    <PageAlert v-if="error" tone="danger">{{ error }}</PageAlert>
    <p v-if="loading && !funding" role="status">正在核对订单资金分配…</p>
    <template v-if="funding">
      <template v-if="funding.change_mode === 'full_term'">
        <p>旧权益结算 {{ money(funding.settlement_minor ?? '0') }} · 用于本单 {{ money(funding.entitlement_minor ?? '0') }} · 结余入余额 {{ money(funding.surplus_minor ?? '0') }}</p>
        <p>旧权益在本次付款履约成功后结算；新套餐从履约成功时获得完整新期间和流量。</p>
      </template>
      <template v-if="!funding.configured">
        <p>当前可用余额 {{ money(funding.available_minor) }}。请选择本次付款方式。</p>
        <template v-if="pendingChoice === null">
          <UiButton type="button" :loading="configuring" @click="configure(true)">先使用余额，不足部分通过渠道支付</UiButton>
          <UiButton type="button" variant="secondary" :disabled="configuring" @click="configure(false)">{{ funding.change_mode === 'full_term' ? '旧权益抵扣后，仅渠道补差' : '全部通过支付渠道' }}</UiButton>
        </template>
        <UiButton v-else type="button" :loading="configuring" @click="configure(pendingChoice)">恢复本次资金分配</UiButton>
      </template>
      <template v-else>
        <p>订单总额 {{ money(funding.amount_minor) }} · 余额分配 {{ money(funding.wallet_minor) }} · 渠道补差 {{ money(funding.cash_minor) }}</p>
        <p v-if="funding.change_mode === 'full_term'">现有余额实际预留 {{ money(funding.reserved_minor ?? '0') }}；旧权益抵扣在结算成功前尚未入账或预留。</p>
        <p v-if="funding.state === 'reserved'">资金分配已冻结，请在 <TimeBadge :value="funding.expires_at" /> 前完成付款。取消、失败或失效会释放预留；迟到的渠道款进入余额。</p>
        <PageAlert v-if="funding.expired || funding.state === 'released'" tone="warning">本次资金分配已失效，请核对余额并重新创建订单。已有渠道扣款会单独核对归属。</PageAlert>
        <template v-if="funding.state === 'reserved' && !funding.expired">
          <UiButton v-if="funding.cash_minor === '0'" type="button" :loading="paying" @click="pay">{{ funding.change_mode === 'full_term' ? '确认结算旧权益并购买新套餐' : funding.amount_minor === '0' ? '确认完成零金额订单' : `确认使用余额支付 ${money(funding.wallet_minor)}` }}</UiButton>
          <CheckoutPayment v-else :key="orderId" name="account.orders.payment" embedded :order-id="orderId" :currency="currency" :channel-amount-minor="funding.cash_minor" :preferred-plugin="preferredPlugin" :reconcile-on-load="Boolean(preferredPlugin)" :open-on-load="Boolean(preferredPlugin)" @paid="changed" />
        </template>
        <UiButton v-if="funding.state === 'reserved'" type="button" variant="secondary" :disabled="paying || canceling" @click="cancel">取消订单并释放预留</UiButton>
        <RouterLink v-if="funding.state === 'released' || funding.state === 'captured'" :to="{ path: '/account/wallet', query: { currency } }">核对余额与流水</RouterLink>
      </template>
    </template>
    <PageAlert v-if="writeError" tone="danger">{{ writeError }}</PageAlert>
    <UiButton type="button" variant="secondary" :disabled="loading || configuring || paying || canceling" @click="load">核对资金分配</UiButton>
    <UiButton v-if="writeError" type="button" variant="secondary" @click="changed">核对本站订单状态</UiButton>
  </section>
</template>
<script setup lang="ts">
import { ref, watch } from 'vue'
import { useAppStore } from '../stores/app'
import { configureOrderFunding, fetchOrderFunding, payOrderFromWallet, type OrderFunding } from '../api/orderFunding'
import { cancelOrder } from '../api/client'
import { useRemoteResource } from '../composables/useRemoteResource'
import { formatWalletMoney } from '../utils/walletMoney'
import { commerceErrorMessage } from '../utils/commerceErrors'
import CheckoutPayment from '../plugins/CheckoutPayment.vue'
import PageAlert from './PageAlert.vue'
import UiButton from './UiButton.vue'
import TimeBadge from './TimeBadge.vue'
const props = defineProps<{ orderId: number; currency: string; preferredPlugin?: string }>()
const emit = defineEmits<{ changed: [] }>()
const app = useAppStore(), configuring = ref(false), paying = ref(false), canceling = ref(false), writeError = ref(''), pendingChoice = ref<boolean | null>(null)
const resource = useRemoteResource<OrderFunding | null>({ initial: () => null, fetch: ({ signal }) => fetchOrderFunding(props.orderId, signal), errorMessage: '订单资金分配暂时无法核对，请重试。' })
const { data: funding, loading, error, load } = resource
function money(value: string) { return formatWalletMoney(value, props.currency) }
function changed() { emit('changed') }
async function configure(useBalance: boolean) {
  if (configuring.value || paying.value || canceling.value) return
  const actor = app.user.id, order = props.orderId
  pendingChoice.value = useBalance; configuring.value = true; writeError.value = ''
  try {
    const result = await configureOrderFunding(order, useBalance)
    if (actor === app.user.id && order === props.orderId) resource.replace(result)
  } catch (cause) { if (actor === app.user.id && order === props.orderId) writeError.value = commerceErrorMessage(cause, '资金分配结果暂无法确认，请先核对或使用原选择恢复，不会自动扣款。') }
  finally { configuring.value = false }
}
async function pay() {
  if (paying.value || configuring.value || canceling.value) return
  const actor = app.user.id, order = props.orderId
  paying.value = true; writeError.value = ''
  try {
    await payOrderFromWallet(order)
    if (actor === app.user.id && order === props.orderId) { changed(); await load() }
  } catch (cause) { if (actor === app.user.id && order === props.orderId) writeError.value = commerceErrorMessage(cause, '付款结果暂无法确认，请先核对本站订单；重新登录不会自动重放付款。') }
  finally { paying.value = false }
}
async function cancel() {
  if (paying.value || canceling.value || configuring.value) return
  const actor = app.user.id, order = props.orderId
  canceling.value = true; writeError.value = ''
  try { await cancelOrder(order); if (actor === app.user.id && order === props.orderId) { changed(); await load() } }
  catch (cause) { if (actor === app.user.id && order === props.orderId) writeError.value = commerceErrorMessage(cause, '取消结果暂无法确认，请核对订单状态。') }
  finally { canceling.value = false }
}
watch(() => [props.orderId, app.user.id], () => { resource.reset(); pendingChoice.value = null; writeError.value = ''; if (props.orderId && app.user.id) void load() }, { immediate: true })
watch(() => funding.value?.state, value => { if (value === 'released' || value === 'captured') changed() })
</script>
