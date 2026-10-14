<template>
  <section class="checkout-payment" :class="{ 'checkout-payment--embedded': embedded }" :data-plugin-slot="name" aria-label="支付方式">
    <header><h3>支付方式</h3></header>
    <p v-if="loading" role="status">正在加载支付方式，订单内容可继续查看。</p>
    <PageAlert v-if="error" tone="danger">{{ error }}</PageAlert>
    <p v-if="!loading && !contributions.length && !error && !paid && !canceled">暂无在线支付方式，可创建订单后联系管理员确认收款。</p>
    <div v-for="contribution in contributions" :key="key(contribution)" class="checkout-payment__provider">
      <strong v-if="contributions.length > 1">{{ contribution.slot.title }}</strong>
      <PluginFrame
        :ref="value => setFrame(key(contribution), value)"
        :plugin-id="contribution.plugin_id" :slot="contribution.slot" surface="account"
        :title="contribution.slot.title" managed :checkout="{ currency, order_id: currentOrder || undefined }"
        @checkout-options="value => receiveOptions(key(contribution), value)"
        @checkout-status="value => receiveStatus(key(contribution), value)"
        @unavailable="receiveStatus(key(contribution), { state: 'error', code: 'session_unavailable' })"
      />
      <PageAlert v-if="statuses[key(contribution)]?.state === 'error'" tone="danger">{{ paymentStatusMessage(statuses[key(contribution)].code) }}</PageAlert>
      <p v-else-if="!options[key(contribution)]" role="status">正在加载支付方式…</p>
      <p v-else-if="!options[key(contribution)].length">暂无可用支付方式，请联系管理员。</p>
      <label v-for="option in options[key(contribution)] || []" :key="option.id">
        <input v-model="selected" type="radio" :value="`${key(contribution)}:${option.id}`" :disabled="busy || option.disabled || paid" name="payment-option">
        {{ option.title }}<span v-if="option.disabled">（暂不可用）</span>
      </label>
    </div>
    <UiButton v-if="canRetry" type="button" variant="secondary" size="sm" :disabled="busy || loading" @click="load">重新加载支付方式</UiButton>
    <div class="checkout-payment__actions">
      <UiButton v-if="currentOrder" :id="paymentTriggerId" type="button" @click="paymentDialogOpen = true">{{ paid ? '查看支付结果' : '查看支付状态' }}</UiButton>
      <UiButton v-else-if="contributions.length || loading || error" :id="paymentTriggerId" type="button" :loading="busy" :disabled="busy || disabled || !choice || choice.option.disabled" @click="pay">确认并支付</UiButton>
      <UiButton v-else-if="createOnly && !currentOrder" type="button" :loading="busy" :disabled="busy || disabled" @click="orderOnly">确认创建订单</UiButton>
    </div>
    <ModalDialog :open="paymentDialogOpen" :title="paid ? '支付已完成' : canceled ? '订单已取消' : '确认支付结果'" size="sm" :description="paid ? '付款结果已核对。' : canceled ? '此订单不可继续支付，请重新下单。' : '请在支付窗口完成付款，返回后核对订单结果。'" :return-focus-selector="`#${paymentTriggerId}`" @close="paymentDialogOpen = false">
      <div class="checkout-payment__dialog">
        <p v-if="currentOrder" class="checkout-payment__state" role="status">订单 #{{ currentOrder }} · {{ paid ? '已完成付款' : canceled ? '已取消，请重新下单。' : waiting ? '等待付款；完成后请核对结果。' : busy ? '正在准备支付或核对结果…' : '支付重试将使用此订单。' }}</p>
        <p v-else role="status">{{ busy ? '正在创建订单并打开支付窗口…' : '订单尚未创建，可重新确认并支付。' }}</p>
        <p v-if="confirmedOrder">订单应付 {{ formatCurrency(confirmedOrder.amount, confirmedOrder.currency) }}</p>
        <p v-if="choice">支付方式：{{ choice.option.title }}</p>
        <PageAlert v-if="paymentError" tone="danger">{{ paymentError }}</PageAlert>
        <p v-if="actionFailure && !actionFailure.retryable" class="checkout-payment__hint">此插件动作不支持直接重试，可更换支付方式或核对本站订单状态。</p>
        <p v-if="paid">ZBoard 已确认订单完成。</p>
        <p v-else-if="!canceled" class="checkout-payment__hint">关闭弹窗不会取消订单，可稍后在“我的订单”继续支付。</p>
      </div>
      <template #footer>
        <UiButton v-if="!paid && !canceled" type="button" variant="secondary" :loading="busy && !checking" :disabled="busy || disabled || !choice || choice.option.disabled || !canRepeat(choice.option.prepare_action)" @click="pay">{{ waiting ? '重新打开支付窗口' : currentOrder ? '继续支付' : '确认并支付' }}</UiButton>
        <UiButton v-if="currentOrder && !paid && !canceled" type="button" :loading="checking" :disabled="busy" @click="reconcile">{{ choice && !canRepeat(choice.option.reconcile_action) ? '刷新订单状态' : '已支付，确认' }}</UiButton>
        <UiButton type="button" variant="ghost" @click="paymentDialogOpen = false">{{ paid || canceled ? '关闭' : '稍后处理' }}</UiButton>
      </template>
    </ModalDialog>
  </section>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, shallowReactive, useId, watch } from 'vue'
import { fetchPluginSlots, type CatalogSlot } from '../api/plugins'
import { fetchAccountOrder } from '../api/client'
import UiButton from '../components/UiButton.vue'
import PluginFrame from './PluginFrame.vue'
import PageAlert from '../components/PageAlert.vue'
import ModalDialog from '../components/ModalDialog.vue'
import { pluginActionFailure, type PluginActionFailure } from '../utils/pluginActionError'
import { formatCurrency } from '../utils/format'
import { startPayment, paymentStatusMessage, type PaymentOption, type PaymentStatus } from './checkout'

const props = withDefaults(defineProps<{ name: string; currency: string; orderId?: number; createOrder?: () => Promise<number>; createOnly?: () => Promise<void>; embedded?: boolean; preferredPlugin?: string; reconcileOnLoad?: boolean; openOnLoad?: boolean; disabled?: boolean }>(), { openOnLoad: true })
const emit = defineEmits<{ paid: [id: number]; created: [id: number] }>()
const paymentTriggerId = `checkout-payment-${useId()}`
type Frame = { checkoutAction(id: number, action: string, payload: Record<string, unknown>): Promise<Record<string, unknown>> }
const frames = shallowReactive(new Map<string, Frame>())
const contributions = ref<CatalogSlot[]>([]), options = reactive<Record<string, PaymentOption[]>>({})
const statuses = reactive<Record<string, PaymentStatus>>({})
const canRetry = computed(() => !busy.value && (Boolean(error.value) || contributions.value.some(c => ['error', 'empty'].includes(statuses[key(c)]?.state))))
const loading = ref(true), busy = ref(false), error = ref(''), selected = ref('')
const confirmedOrder = ref<{ amount: number; currency: string }>()
const currentOrder = ref(props.orderId || 0), waiting = ref(false), paid = ref(false)
const paymentDialogOpen = ref(Boolean(props.orderId) && props.openOnLoad !== false), paymentError = ref(''), checking = ref(false)
const actionFailure = ref<PluginActionFailure>(), rejectedAction = ref('')
const canceled = ref(false)
const optionTimers = new Map<string, ReturnType<typeof setTimeout>>()
function clearOptionTimers() { for (const timer of optionTimers.values()) clearTimeout(timer); optionTimers.clear() }
function finishOptions(id: string) { clearTimeout(optionTimers.get(id)); optionTimers.delete(id) }
let controller = new AbortController(), generation = 0, restored = false
function savedChoice() {
  if (!currentOrder.value) return undefined
  try { const value = JSON.parse(localStorage.getItem(`zboard-payment-choice:${currentOrder.value}`) || 'null'); return value && typeof value.plugin === 'string' && typeof value.option === 'string' ? value : undefined } catch { return undefined }
}
const preferred = computed(() => props.preferredPlugin || savedChoice()?.plugin || '')
const key = (value: CatalogSlot) => `${value.plugin_id}:${value.slot.id}`
const choice = computed(() => {
  for (const c of contributions.value) for (const option of options[key(c)] || []) {
    if (`${key(c)}:${option.id}` === selected.value) return { contribution: c, option, frame: frames.get(key(c)) }
  }
  return undefined
})
function setFrame(id: string, value: unknown) { if (value) frames.set(id, value as Frame); else frames.delete(id) }
function actionKey(action: string) { return `${selected.value}:${action}` }
function canRepeat(action: string) { return actionFailure.value?.retryable !== false || rejectedAction.value !== actionKey(action) }
function recordActionFailure(cause: unknown, action: string) {
  actionFailure.value = pluginActionFailure(cause)
  rejectedAction.value = actionFailure.value ? actionKey(action) : ''
}
function receiveStatus(id: string, status: PaymentStatus) {
  if (!contributions.value.some(c => key(c) === id)) return
  if (status.state !== 'loading') finishOptions(id)
  else if (!optionTimers.has(id)) {
    const current = generation
    optionTimers.set(id, setTimeout(() => { if (current === generation && !options[id]) receiveStatus(id, { state: 'error', code: 'bridge_timeout' }) }, 35_000))
  }
  statuses[id] = status
  if (status.state === 'error' || status.state === 'loading') delete options[id]
}
async function orderOnly() {
  if (busy.value || props.disabled || loading.value || error.value || contributions.value.length || !props.createOnly) return
  busy.value = true
  try { await props.createOnly() }
  finally { busy.value = false }
}
function receiveOptions(id: string, values: PaymentOption[]) {
  if (!contributions.value.some(c => key(c) === id)) return
  finishOptions(id)
  statuses[id] = { state: values.length ? 'ready' : 'empty' }
  options[id] = values
  const contribution = contributions.value.find(c => key(c) === id)
  if (!choice.value && (!preferred.value || preferred.value === contribution?.plugin_id)) {
    const saved = savedChoice()
    const option = values.find(item => item.id === saved?.option && (currentOrder.value || !item.disabled)) || values.find(item => !item.disabled)
    if (option) selected.value = `${id}:${option.id}`
  }
  if (props.reconcileOnLoad && props.orderId && !restored && !paid.value && choice.value?.contribution.plugin_id === props.preferredPlugin) {
    restored = true
    void reconcile()
  }
}
async function load() {
  if (busy.value) return
  clearOptionTimers()
  controller.abort(); controller = new AbortController(); const current = ++generation
  restored = false
  loading.value = true; error.value = ''; contributions.value = []; selected.value = ''
  for (const id of Object.keys(options)) delete options[id]
  for (const id of Object.keys(statuses)) delete statuses[id]
  if (currentOrder.value) void refreshOrder(currentOrder.value, current).catch(() => {
    if (current === generation) paymentError.value = '订单状态暂时无法读取，请重新核对。'
  })
  try {
    const values = await fetchPluginSlots('account', props.name, controller.signal)
    if (current === generation) {
      contributions.value = values
      for (const c of values) {
        const id = key(c)
        optionTimers.set(id, setTimeout(() => {
          if (current === generation && !options[id]) receiveStatus(id, { state: 'error', code: 'bridge_timeout' })
        }, 35_000))
      }
    }
  } catch { if (current === generation) error.value = '支付方式加载失败，请局部重试。' }
  finally { if (current === generation) loading.value = false }
}
async function refreshOrder(id: number, current: number) {
  const order = await fetchAccountOrder(id, { signal: controller.signal })
  if (current !== generation) return undefined
  if (typeof order.payable_amount === 'number') confirmedOrder.value = { amount: order.payable_amount, currency: order.currency }
  canceled.value = order.status === 'canceled'
  if (order.status === 'paid' && !paid.value) {
    paid.value = true; waiting.value = false; paymentError.value = ''; actionFailure.value = undefined; emit('paid', id)
  }
  return order
}
function pay() {
  const value = choice.value
  if (busy.value || props.disabled || !value?.frame || value.option.disabled || paid.value || canceled.value || !canRepeat(value.option.prepare_action)) return
  actionFailure.value = undefined; rejectedAction.value = ''
  paymentError.value = ''; busy.value = true; paymentDialogOpen.value = true
  const current = generation
  let attempt: Promise<number>
  try {
    // No await before startPayment: the parent button owns user activation.
    attempt = startPayment(async () => {
      const id = currentOrder.value || await props.createOrder?.()
      if (!id) throw new Error('没有可支付的业务订单。')
      if (current !== generation) throw new Error('支付页面已变化，请在订单页恢复。')
      currentOrder.value = id; emit('created', id)
      try { localStorage.setItem(`zboard-payment-choice:${id}`, JSON.stringify({ plugin: value.contribution.plugin_id, option: value.option.id })) } catch { /* Navigation hint; never payment authority. */ }
      return id
    }, async id => {
      const order = await refreshOrder(id, current)
      if (current !== generation) throw new Error('支付页面已变化，请在订单页恢复。')
      if (order?.status === 'paid') throw new Error('此订单已完成付款。')
      if (order?.status === 'canceled') throw new Error('此订单已取消，请重新下单。')
      const result = await value.frame!.checkoutAction(id, value.option.prepare_action, { option_id: value.option.id })
      if (current !== generation) throw new Error('支付页面已变化，请在订单页恢复。')
      return result
    })
  } catch (cause: any) { paymentError.value = cause?.message || '无法打开支付窗口。'; busy.value = false; return }
  void attempt.then(() => { if (current === generation) waiting.value = true })
    .catch(cause => { if (current === generation && !paid.value) { recordActionFailure(cause, value.option.prepare_action); paymentError.value = cause?.response?.data?.message || cause?.message || '支付准备失败，可使用同一订单重试。' } })
    .finally(() => { if (current === generation) busy.value = false })
}
async function reconcile() {
  const value = choice.value
  if (busy.value || !currentOrder.value || paid.value) return
  busy.value = true; checking.value = true; paymentError.value = ''; const current = generation, orderId = currentOrder.value
  try {
    const before = await refreshOrder(orderId, current)
    if (current !== generation || before?.status === 'paid') return
    if (before?.status === 'canceled') { paymentError.value = '此订单已取消，请重新下单。'; return }
    let failure: any
    // A callback may have committed even if the plugin response is lost.
    // Host status remains readable after the extension's authorization expires.
    if (value?.frame && canRepeat(value.option.reconcile_action)) {
      try { await value.frame.checkoutAction(orderId, value.option.reconcile_action, { option_id: value.option.id }) }
      catch (cause) { failure = cause; recordActionFailure(cause, value.option.reconcile_action) }
    } else if (actionFailure.value && value && !canRepeat(value.option.reconcile_action)) {
      failure = new Error(actionFailure.value.message)
    } else { failure = new Error('支付扩展暂不可用，本站尚未确认到账。') }
    if (current !== generation) return
    const after = await refreshOrder(orderId, current)
    if (current !== generation || after?.status === 'paid') return
    waiting.value = true
    paymentError.value = after?.status === 'canceled' ? '此订单已取消，请重新下单。' : failure?.response?.data?.message || failure?.message || '尚未确认到账，请稍后再核对。'
  } catch (cause: any) { if (current === generation) paymentError.value = cause?.response?.data?.message || cause?.message || '核对暂时不可用，请稍后重试。' }
  finally { if (current === generation) { busy.value = false; checking.value = false } }
}
watch(() => [props.name, props.orderId, props.preferredPlugin], () => {
  generation++; controller.abort(); busy.value = false; confirmedOrder.value = undefined
  paymentDialogOpen.value = Boolean(props.orderId) && props.openOnLoad !== false; paymentError.value = ''; checking.value = false
  actionFailure.value = undefined; rejectedAction.value = ''
  currentOrder.value = props.orderId || 0; waiting.value = false; paid.value = false; canceled.value = false; restored = false; void load()
})
watch(selected, () => { if (actionFailure.value) paymentError.value = ''; actionFailure.value = undefined; rejectedAction.value = '' })
onMounted(load)
onBeforeUnmount(() => { generation++; controller.abort(); clearOptionTimers() })
</script>
<style scoped>
.checkout-payment { display: grid; gap: 12px; min-width: 0; padding: 16px; border: 1px solid var(--line); border-radius: 10px; }
.checkout-payment--embedded { padding: 16px 0 0; border: 0; border-top: 1px solid var(--line); border-radius: 0; }
.checkout-payment--embedded :deep(.ui-button) { width: 100%; }
.checkout-payment--embedded .checkout-payment__actions { display: grid; }
.checkout-payment header, .checkout-payment__actions { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.checkout-payment header { justify-content: space-between; }
.checkout-payment h3, .checkout-payment p { margin: 0; }
.checkout-payment__provider { display: grid; gap: 8px; min-width: 0; }
.checkout-payment__provider label { display: flex; align-items: center; gap: 8px; padding: 10px; border: 1px solid var(--line); border-radius: var(--radius-md); cursor: pointer; }
.checkout-payment__provider label:has(input:checked) { border-color: var(--primary); background: var(--primary-soft); }
.checkout-payment h3 { font-size: 14px; }
.checkout-payment__state { padding: 12px; background: var(--surface-soft); border-radius: 8px; }
.checkout-payment__dialog { display: grid; gap: 12px; }
.checkout-payment__dialog p { margin: 0; }
.checkout-payment__hint { color: var(--text-secondary); font-size: 12px; line-height: 1.6; }
</style>
