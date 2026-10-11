<template>
  <section class="checkout-payment" :data-plugin-slot="name" aria-label="支付方式">
    <header><h3>支付方式</h3><UiButton variant="secondary" size="sm" :disabled="busy || loading" @click="load">重新加载支付方式</UiButton></header>
    <p v-if="loading" role="status">正在加载支付方式，订单内容可继续查看。</p>
    <p v-if="error" role="alert">{{ error }}</p>
    <p v-if="!loading && !contributions.length && !error && !paid && !canceled">暂无在线支付方式，可创建订单后联系管理员确认收款。</p>
    <div v-for="contribution in contributions" :key="key(contribution)" class="checkout-payment__provider">
      <strong>{{ contribution.slot.title }}</strong>
      <PluginFrame
        :ref="value => setFrame(key(contribution), value)"
        :plugin-id="contribution.plugin_id" :slot="contribution.slot" surface="account"
        :title="contribution.slot.title" :checkout="{ currency, order_id: currentOrder || undefined }"
        @checkout-options="value => receiveOptions(key(contribution), value)"
        @unavailable="delete options[key(contribution)]"
      />
      <p v-if="!options[key(contribution)]" role="status">等待扩展提供可用方式；如未响应，可重新加载。</p>
      <label v-for="option in options[key(contribution)] || []" :key="option.id">
        <input v-model="selected" type="radio" :value="`${key(contribution)}:${option.id}`" :disabled="busy || option.disabled || paid" name="payment-option">
        {{ option.title }}<span v-if="option.disabled">（暂不可用）</span>
      </label>
    </div>
    <p v-if="currentOrder" class="checkout-payment__state" role="status">订单 #{{ currentOrder }} · {{ paid ? '已完成付款' : canceled ? '已取消，请重新下单。' : waiting ? '等待付款；完成后请核对结果。' : '支付重试将使用此订单。' }}</p>
    <p v-if="confirmedOrder">订单应付 {{ formatCurrency(confirmedOrder.amount, confirmedOrder.currency) }}</p>
    <div v-if="contributions.length || currentOrder" class="checkout-payment__actions">
      <UiButton v-if="contributions.length" type="button" :loading="busy" :disabled="busy || disabled || !choice || choice.option.disabled || paid || canceled" @click="pay">{{ waiting ? '重新打开支付窗口' : '前往支付' }}</UiButton>
      <UiButton v-if="currentOrder && !paid" type="button" variant="secondary" :disabled="busy" @click="reconcile">已支付，确认</UiButton>
    </div>
    <p v-if="paid">ZBoard 已确认订单完成。</p>
  </section>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, shallowReactive, watch } from 'vue'
import { fetchPluginSlots, type CatalogSlot } from '../api/plugins'
import { fetchAccountOrder } from '../api/client'
import UiButton from '../components/UiButton.vue'
import PluginFrame from './PluginFrame.vue'
import { formatCurrency } from '../utils/format'
import { startPayment, type PaymentOption } from './checkout'

const props = defineProps<{ name: string; currency: string; orderId?: number; createOrder?: () => Promise<number>; preferredPlugin?: string; reconcileOnLoad?: boolean; disabled?: boolean }>()
const emit = defineEmits<{ paid: [id: number]; created: [id: number] }>()
type Frame = { checkoutAction(id: number, action: string, payload: Record<string, unknown>): Promise<Record<string, unknown>> }
const frames = shallowReactive(new Map<string, Frame>())
const contributions = ref<CatalogSlot[]>([]), options = reactive<Record<string, PaymentOption[]>>({})
const loading = ref(false), busy = ref(false), error = ref(''), selected = ref('')
const confirmedOrder = ref<{ amount: number; currency: string }>()
const currentOrder = ref(props.orderId || 0), waiting = ref(false), paid = ref(false)
const canceled = ref(false)
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
function receiveOptions(id: string, values: PaymentOption[]) {
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
  controller.abort(); controller = new AbortController(); const current = ++generation
  restored = false
  loading.value = true; error.value = ''; contributions.value = []; selected.value = ''
  for (const id of Object.keys(options)) delete options[id]
  if (currentOrder.value) void refreshOrder(currentOrder.value, current).catch(() => {
    if (current === generation) error.value = '订单状态暂时无法读取，请重新核对。'
  })
  try {
    const values = await fetchPluginSlots('account', props.name, controller.signal)
    if (current === generation) contributions.value = values
  } catch { if (current === generation) error.value = '支付方式加载失败，请局部重试。' }
  finally { if (current === generation) loading.value = false }
}
async function refreshOrder(id: number, current: number) {
  const order = await fetchAccountOrder(id, { signal: controller.signal })
  if (current !== generation) return undefined
  if (typeof order.payable_amount === 'number') confirmedOrder.value = { amount: order.payable_amount, currency: order.currency }
  canceled.value = order.status === 'canceled'
  if (order.status === 'paid' && !paid.value) {
    paid.value = true; waiting.value = false; error.value = ''; emit('paid', id)
  }
  return order
}
function pay() {
  const value = choice.value
  if (busy.value || props.disabled || !value?.frame || value.option.disabled || paid.value || canceled.value) return
  error.value = ''; busy.value = true
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
  } catch (cause: any) { error.value = cause?.message || '无法打开支付窗口。'; busy.value = false; return }
  void attempt.then(() => { if (current === generation) waiting.value = true })
    .catch(cause => { if (current === generation && !paid.value) error.value = cause?.response?.data?.message || cause?.message || '支付准备失败，可使用同一订单重试。' })
    .finally(() => { if (current === generation) busy.value = false })
}
async function reconcile() {
  const value = choice.value
  if (busy.value || !currentOrder.value || paid.value) return
  busy.value = true; error.value = ''; const current = generation, orderId = currentOrder.value
  try {
    const before = await refreshOrder(orderId, current)
    if (current !== generation || before?.status === 'paid') return
    if (before?.status === 'canceled') { error.value = '此订单已取消，请重新下单。'; return }
    let failure: any
    // A callback may have committed even if the plugin response is lost.
    // Host status remains readable after the extension's authorization expires.
    if (value?.frame) {
      try { await value.frame.checkoutAction(orderId, value.option.reconcile_action, { option_id: value.option.id }) }
      catch (cause) { failure = cause }
    } else { failure = new Error('支付扩展暂不可用，本站尚未确认到账。') }
    if (current !== generation) return
    const after = await refreshOrder(orderId, current)
    if (current !== generation || after?.status === 'paid') return
    waiting.value = true
    error.value = after?.status === 'canceled' ? '此订单已取消，请重新下单。' : failure?.response?.data?.message || failure?.message || '尚未确认到账，请稍后再核对。'
  } catch (cause: any) { if (current === generation) error.value = cause?.response?.data?.message || cause?.message || '核对暂时不可用，请稍后重试。' }
  finally { if (current === generation) busy.value = false }
}
watch(() => [props.name, props.orderId, props.preferredPlugin], () => {
  generation++; controller.abort(); busy.value = false; confirmedOrder.value = undefined
  currentOrder.value = props.orderId || 0; waiting.value = false; paid.value = false; canceled.value = false; restored = false; void load()
})
onMounted(load)
onBeforeUnmount(() => { generation++; controller.abort() })
</script>
<style scoped>
.checkout-payment { display: grid; gap: 12px; min-width: 0; padding: 16px; border: 1px solid var(--line); border-radius: 10px; }
.checkout-payment header, .checkout-payment__actions { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.checkout-payment header { justify-content: space-between; }
.checkout-payment h3, .checkout-payment p { margin: 0; }
.checkout-payment__provider { display: grid; gap: 8px; min-width: 0; }
.checkout-payment__provider label { display: flex; align-items: center; gap: 8px; }
.checkout-payment__state { padding: 12px; background: var(--surface-soft); border-radius: 8px; }
</style>
