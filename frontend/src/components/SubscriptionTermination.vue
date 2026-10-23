<template>
  <UiSection :title="`退订订阅 #${subscriptionId}`" :description="administrative ? '整份终止剩余权益；默认按残值退款，管理员可覆盖金额，包括0元。' : '终止本份订阅的全部剩余权益，按实际成交残值退入站内余额。'">
    <div class="panel-body stack">
      <PageAlert v-if="error" tone="danger" title="退订需要核对">{{ error }}</PageAlert>
      <PageAlert v-if="receipt" tone="success" title="订阅已终止">
        <template v-if="receipt.amount_minor === '0'">订阅已终止，本次未退款。</template><template v-else>{{ money(receipt.amount_minor, receipt.currency) }} 已进入余额。</template>{{ receipt.record_kind === 'entitlement_mutation' ? '权益终止记录' : '结算' }} #{{ receipt.id }}。
        节点撤权等待核验；当前连接终止取决于节点的实际能力。其他订阅继续独立使用。
      </PageAlert>
      <template v-else>
        <p>{{ administrative ? '默认退款为剩余价值，实际金额由管理员确认；0元只终止权益，不退款。' : '退的是剩余价值，不是原支付全款；不保留部分套餐。' }}确认后不再保留续费恢复窗口。余额退款不提供提现；历史订单和计量记录保留。</p>
        <PageAlert v-if="quote?.valuation_status === 'unavailable'" tone="warning" title="默认残值未知">{{ quote.message }}本次只能明确确认0元终止，不提供任何自动退款。</PageAlert>
        <p v-else-if="quote">本周期残值 {{ money(quote.current_minor, quote.currency) }}，后续已付周期 {{ money(quote.future_minor, quote.currency) }}，合计 {{ money(quote.total_minor, quote.currency) }}。确认时会重新校验用量和权益。</p>
        <template v-if="administrative && quote && !intent">
          <p v-if="quote.valuation_status !== 'unavailable'">可退实付上限 {{ money(quote.max_refund_minor || '', quote.currency) }}；退款进入订阅所属用户余额，不发起外部资金退款。</p>
          <FormField v-slot="{ controlAttrs }" label="实际退款金额（可为0）" :name="`termination-amount-${subscriptionId}`" :hint="quote.valuation_status === 'unavailable' ? '只允许明确输入0，不推断未知币种或成交金额。' : quote.currency + '，最多两位小数。'"><UiInput v-model="amount" v-bind="controlAttrs" inputmode="decimal" /></FormField>
          <FormField v-slot="{ controlAttrs }" label="管理员退订原因" :name="`termination-reason-${subscriptionId}`" hint="3–400个UTF-8字节；默认残值与实际金额均保留审计。"><UiInput v-model="reason" v-bind="controlAttrs" maxlength="400" /></FormField>
        </template>
        <p v-if="administrative && intent">已保留管理员审批：实际退款 {{ intent.quote.valuation_status === 'unavailable' ? '0（不退款）' : money(String(intent.request.amount_minor), intent.quote.currency) }}，原默认残值 {{ money(intent.quote.total_minor, intent.quote.currency) }}；原因：{{ intent.request.reason }}。</p>
        <p v-if="intent">已有一笔待核对请求。重新登录或刷新页面后，需要再次点击才能重试原请求。</p>
        <UiButton v-if="!intent || knownConflict" type="button" variant="secondary" :disabled="busy || blocked" @click="loadQuote">{{ quote ? '重新核对残值' : '核对退订残值' }}</UiButton>
        <UiButton v-if="quote && !knownConflict" type="button" :disabled="busy || blocked || (administrative && !intent && !validApproval)" :loading="busy" @click="submitTermination">{{ intent ? '核对并重试原退订请求' : administrative && approvalMinor === 0 ? '确认终止订阅（不退款）' : '确认终止订阅并退款至余额' }}</UiButton>
      </template>
    </div>
  </UiSection>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useAppStore } from '../stores/app'
import { fetchSubscriptionValue, terminateSubscription, fetchAdministrativeSubscriptionValue, administrativelyTerminateSubscription, type AdministrativeSubscriptionTerminationRequest, type AdministrativeSubscriptionValueQuote, type SubscriptionValueQuote, type SubscriptionTerminationRequest, type SubscriptionTerminationReceipt } from '../api/subscriptionValue'
import { newCheckoutIntent } from '../composables/checkoutOrder'
import { commerceErrorMessage } from '../utils/commerceErrors'
import { formatWalletMoney, parseWalletAmount, walletMajor } from '../utils/walletMoney'
import { isUtf8LengthInRange } from '../utils/validation'
import UiInput from './UiInput.vue'
import FormField from './FormField.vue'
import UiSection from './UiSection.vue'
import UiButton from './UiButton.vue'
import PageAlert from './PageAlert.vue'
const props = defineProps<{ subscriptionId: number; administrative?: boolean }>(), emit = defineEmits<{ settled: [] }>()
const app = useAppStore(), actor = computed(() => app.user?.id || 0)
type Quote = Omit<SubscriptionValueQuote, 'root_order_id' | 'current_minor' | 'future_minor' | 'total_minor'> & Partial<AdministrativeSubscriptionValueQuote>
interface Intent { request: SubscriptionTerminationRequest & Partial<AdministrativeSubscriptionTerminationRequest>; quote: Quote; receipt?: SubscriptionTerminationReceipt }
const quote = ref<Quote | null>(null), receipt = ref<SubscriptionTerminationReceipt | null>(null), intent = ref<Intent | null>(null)
const busy = ref(false), blocked = ref(false), knownConflict = ref(false), error = ref('')
const amount = ref(''), reason = ref('')
const approvalMinor = computed(() => /^0(?:\.0{1,2})?$/.test(amount.value.trim()) ? 0 : parseWalletAmount(amount.value))
const validApproval = computed(() => approvalMinor.value !== null && /^\d+$/.test(quote.value?.max_refund_minor || '') && BigInt(approvalMinor.value!) <= BigInt(quote.value!.max_refund_minor!) && isUtf8LengthInRange(reason.value.trim(), 3, 400))
const scope = () => `zboard:${props.administrative ? 'admin-unsubscribe' : 'unsubscribe'}:${actor.value}:${props.subscriptionId}`
let generation = 0
function money(value: string | null | undefined, currency: string) { return typeof value === 'string' ? formatWalletMoney(value, currency) : '金额未知' }
watch([actor, () => props.subscriptionId, () => props.administrative, () => app.user?.isAdmin], () => {
  generation++; quote.value = null; receipt.value = null; intent.value = null; knownConflict.value = false; error.value = ''; blocked.value = true
  amount.value = ''; reason.value = ''; if (!actor.value || props.administrative && !app.user?.isAdmin) return
  try {
    const raw = localStorage.getItem(scope())
    if (raw) {
      const saved: Intent = JSON.parse(raw)
      if (!saved?.request?.idempotency_key || !/^[a-f0-9]{64}$/.test(saved.request.quote_fingerprint) || saved.quote?.subscription_id !== props.subscriptionId || saved.quote.quote_fingerprint !== saved.request.quote_fingerprint || (saved.receipt && saved.receipt.subscription_id !== props.subscriptionId)) throw new Error('退订请求记录不完整，请先核对订阅与余额流水。')
      if (props.administrative && (!Number.isSafeInteger(saved.request.amount_minor) || saved.request.amount_minor! < 0 || !/^\d+$/.test(saved.quote.max_refund_minor || '') || BigInt(saved.request.amount_minor!) > BigInt(saved.quote.max_refund_minor!) || !isUtf8LengthInRange(saved.request.reason || '', 3, 400) || saved.receipt && saved.receipt.amount_minor !== String(saved.request.amount_minor)) || !props.administrative && (saved.request.amount_minor !== undefined || saved.request.reason !== undefined)) throw new Error('退订请求权限范围不一致。')
      if (saved.quote.valuation_status === 'unavailable' && (saved.request.amount_minor !== 0 || saved.quote.total_minor !== null || saved.quote.current_minor !== null || saved.quote.future_minor !== null || saved.quote.max_refund_minor !== '0')) throw new Error('未知残值的原请求并非明确0元。')
      intent.value = saved; quote.value = saved.quote; receipt.value = saved.receipt || null
    }
    blocked.value = false
  } catch { error.value = '退订请求记录无法读取，请先通过工单核对，避免重复结算。' }
}, { immediate: true })
async function loadQuote() {
  if (busy.value || blocked.value || !actor.value || (intent.value && !knownConflict.value)) return
  const version = generation, id = props.subscriptionId, key = scope(); busy.value = true; error.value = ''
  try {
    const next: Quote = props.administrative ? await fetchAdministrativeSubscriptionValue(id) : await fetchSubscriptionValue(id)
    if (generation !== version || key !== scope()) return
    if (next.subscription_id !== id) throw new Error('报价所属订阅不一致，请刷新后重试。')
    // A confirmed conflict has no committed financial result. Discard that
    // request only after the user explicitly asks for a fresh quote.
    if (knownConflict.value) { localStorage.removeItem(key); intent.value = null }
    if (props.administrative) {
      if (next.valuation_status === 'unavailable') {
        if (next.total_minor !== null || next.current_minor !== null || next.future_minor !== null || next.max_refund_minor !== '0') throw new Error('未知残值审批范围无法核对。')
        amount.value = ''
      } else {
        if (!/^\d+$/.test(next.max_refund_minor || '') || typeof next.total_minor !== 'string' || !/^\d+$/.test(next.total_minor) || BigInt(next.total_minor) > BigInt(next.max_refund_minor!)) throw new Error('退款上限无法核对，请重新核对。')
        amount.value = walletMajor(next.total_minor)
      }
    }
    quote.value = next; knownConflict.value = false
  } catch (cause) { if (generation === version) error.value = commerceErrorMessage(cause, '残值暂时无法核对，请稍后重试。') }
  finally { busy.value = false }
}
async function submitTermination() {
  if (busy.value || blocked.value || knownConflict.value || !quote.value || !actor.value || props.administrative && (!app.user?.isAdmin || !intent.value && !validApproval.value)) return
  const version = generation, id = props.subscriptionId, key = scope(); busy.value = true; error.value = ''
  try {
    let attempt = intent.value
    if (!attempt) {
      const request: Intent['request'] = { quote_fingerprint: quote.value.quote_fingerprint, idempotency_key: newCheckoutIntent() }
      if (props.administrative) { request.amount_minor = approvalMinor.value!; request.reason = reason.value.trim() }
      attempt = { request, quote: quote.value }
    }
    // Persist before POST. A failed persistence operation prevents the write.
    localStorage.setItem(key, JSON.stringify(attempt)); intent.value = attempt
    const result = props.administrative ? await administrativelyTerminateSubscription(id, attempt.request as AdministrativeSubscriptionTerminationRequest) : await terminateSubscription(id, attempt.request)
    if (attempt.quote.valuation_status === 'unavailable' && (result.record_kind !== 'entitlement_mutation' || result.amount_minor !== '0' || result.wallet_entry_id !== undefined)) throw new Error('0元权益终止记录无法核对，请核对原请求。')
    if (result.subscription_id !== id || result.currency !== attempt.quote.currency || result.status !== 'canceled' || props.administrative && result.amount_minor !== String(attempt.request.amount_minor)) throw new Error('退订结果所属订阅不一致，请核对账务。')
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
