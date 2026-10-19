<template>
  <UiSection :title="`退订订阅 #${subscriptionId}`" description="终止本份订阅，按实际成交残值退入站内余额。">
    <div class="panel-body stack">
      <PageAlert v-if="error" tone="danger" title="退订需要核对">{{ error }}</PageAlert>
      <PageAlert v-if="receipt" tone="success" title="退订已结算">
        {{ money(receipt.amount_minor, receipt.currency) }} 已进入余额。结算 #{{ receipt.id }}。
        节点撤权等待核验；当前连接终止取决于节点的实际能力。其他订阅继续独立使用。
      </PageAlert>
      <template v-else>
        <p>确认后不再保留续费恢复窗口。余额退款不提供提现；历史订单和计量记录保留。</p>
        <p v-if="quote">本周期残值 {{ money(quote.current_minor, quote.currency) }}，后续已付周期 {{ money(quote.future_minor, quote.currency) }}，合计 {{ money(quote.total_minor, quote.currency) }}。确认时会重新校验用量和权益。</p>
        <p v-if="intent">已有一笔待核对请求。重新登录或刷新页面后，需要再次点击才能重试原请求。</p>
        <UiButton v-if="!intent || knownConflict" type="button" variant="secondary" :disabled="busy || blocked" @click="loadQuote">{{ quote ? '重新核对残值' : '核对退订残值' }}</UiButton>
        <UiButton v-if="quote && !knownConflict" type="button" :disabled="busy || blocked" :loading="busy" @click="submitTermination">{{ intent ? '核对并重试原退订请求' : '确认终止订阅并退款至余额' }}</UiButton>
      </template>
    </div>
  </UiSection>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useAppStore } from '../stores/app'
import { fetchSubscriptionValue, terminateSubscription, type SubscriptionValueQuote, type SubscriptionTerminationRequest, type SubscriptionTerminationReceipt } from '../api/subscriptionValue'
import { newCheckoutIntent } from '../composables/checkoutOrder'
import { commerceErrorMessage } from '../utils/commerceErrors'
import { formatWalletMoney } from '../utils/walletMoney'
import UiSection from './UiSection.vue'
import UiButton from './UiButton.vue'
import PageAlert from './PageAlert.vue'
const props = defineProps<{ subscriptionId: number }>(), emit = defineEmits<{ settled: [] }>()
const app = useAppStore(), actor = computed(() => app.user?.id || 0)
interface Intent { request: SubscriptionTerminationRequest; quote: SubscriptionValueQuote; receipt?: SubscriptionTerminationReceipt }
const quote = ref<SubscriptionValueQuote | null>(null), receipt = ref<SubscriptionTerminationReceipt | null>(null), intent = ref<Intent | null>(null)
const busy = ref(false), blocked = ref(false), knownConflict = ref(false), error = ref('')
const scope = () => `zboard:unsubscribe:${actor.value}:${props.subscriptionId}`
let generation = 0
function money(value: string, currency: string) { return formatWalletMoney(value, currency) }
watch(() => [actor.value, props.subscriptionId], () => {
  generation++; quote.value = null; receipt.value = null; intent.value = null; knownConflict.value = false; error.value = ''; blocked.value = true
  if (!actor.value) return
  try {
    const raw = localStorage.getItem(scope())
    if (raw) {
      const saved: Intent = JSON.parse(raw)
      if (!saved?.request?.idempotency_key || !/^[a-f0-9]{64}$/.test(saved.request.quote_fingerprint) || saved.quote?.subscription_id !== props.subscriptionId || saved.quote.quote_fingerprint !== saved.request.quote_fingerprint || (saved.receipt && saved.receipt.subscription_id !== props.subscriptionId)) throw new Error('退订请求记录不完整，请先核对订阅与余额流水。')
      intent.value = saved; quote.value = saved.quote; receipt.value = saved.receipt || null
    }
    blocked.value = false
  } catch { error.value = '退订请求记录无法读取，请先通过工单核对，避免重复结算。' }
}, { immediate: true })
async function loadQuote() {
  if (busy.value || blocked.value || !actor.value || (intent.value && !knownConflict.value)) return
  const version = generation, id = props.subscriptionId, key = scope(); busy.value = true; error.value = ''
  try {
    const next = await fetchSubscriptionValue(id)
    if (generation !== version || key !== scope()) return
    if (next.subscription_id !== id) throw new Error('报价所属订阅不一致，请刷新后重试。')
    // A confirmed conflict has no committed financial result. Discard that
    // request only after the user explicitly asks for a fresh quote.
    if (knownConflict.value) { localStorage.removeItem(key); intent.value = null }
    quote.value = next; knownConflict.value = false
  } catch (cause) { if (generation === version) error.value = commerceErrorMessage(cause, '残值暂时无法核对，请稍后重试。') }
  finally { busy.value = false }
}
async function submitTermination() {
  if (busy.value || blocked.value || knownConflict.value || !quote.value || !actor.value) return
  const version = generation, id = props.subscriptionId, key = scope(); busy.value = true; error.value = ''
  try {
    const attempt = intent.value || { request: { quote_fingerprint: quote.value.quote_fingerprint, idempotency_key: newCheckoutIntent() }, quote: quote.value }
    // Persist before POST. A failed persistence operation prevents the write.
    localStorage.setItem(key, JSON.stringify(attempt)); intent.value = attempt
    const result = await terminateSubscription(id, attempt.request)
    if (result.subscription_id !== id) throw new Error('退订结果所属订阅不一致，请核对账务。')
    attempt.receipt = result; localStorage.setItem(key, JSON.stringify(attempt))
    if (generation !== version || key !== scope()) return
    receipt.value = result; emit('settled')
  } catch (cause: any) {
    if (generation !== version) return
    knownConflict.value = cause?.response?.status === 409
    error.value = commerceErrorMessage(cause, '退订结果暂无法确认，请核对后明确重试原请求。')
  } finally { busy.value = false }
}
</script>
