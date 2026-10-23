<template>
  <UiSection title="获批消费退款" description="消费退款统一进入站内余额；原资金来源保留，不发起渠道退款或提现。">
    <PageAlert v-if="!eligible" tone="warning" title="当前订单不能执行消费退款">仅支持已有明确结束记录且已履约的历史消费订单。有效订阅请在订阅页整份退订，按剩余价值退款并终止全部剩余权益；不退原支付全款，也不保留部分套餐。</PageAlert>
    <template v-else>
      <p>来源订单 #{{ order.id }} · 用户 #{{ order.user_id }} · {{ order.currency }}。已退款、已换购或退订转换的价值不能再次退还；最终可退预算由服务器核对。</p>
      <p>原始渠道收款请核对本订单的“渠道收款归属”。本次退款不会终止、恢复或调整该用户当前的其他权益。</p>
      <PageAlert v-if="writeError" tone="danger" title="退款操作需要核对">{{ writeError }}</PageAlert>
      <form data-test="consumption-refund" class="stack" @submit.prevent="requestApproval">
        <FormField v-slot="{ controlAttrs }" label="获批退款金额" :name="`consumption-refund-amount-${order.id}`" :hint="`${order.currency}，最多两位小数；不包含优惠金额。`">
          <UiInput v-model="amount" v-bind="controlAttrs" inputmode="decimal" :disabled="locked" />
        </FormField>
        <FormField v-slot="{ controlAttrs }" label="原订单用户的工单编号" :name="`consumption-refund-ticket-${order.id}`">
          <UiInput v-model="ticket" v-bind="controlAttrs" inputmode="numeric" :disabled="locked" />
        </FormField>
        <FormField v-slot="{ controlAttrs }" label="获批原因（用户可见）" :name="`consumption-refund-reason-${order.id}`" hint="3–400 个 UTF-8 字节。">
          <UiInput v-model="reason" v-bind="controlAttrs" maxlength="400" :disabled="locked" />
        </FormField>
        <p v-if="intent">退款请求标识：<code>{{ intent.request.idempotency_key }}</code></p>
        <p v-if="intent && !intent.refund_id && !intent.rejected_status">原请求已保留。请核对工单、原订单及账务；明确重试将使用完全相同的金额、原因和标识。本页面不会自动重试。</p>
        <p v-if="intent?.rejected_status === 400">服务器已明确拒绝这份申请，未批准本次退款。请核对错误后选择修改；不会自动提交新申请。</p>
        <UiButton type="submit" :loading="saving" :disabled="blocked || !administrator || Boolean(intent?.refund_id) || (!intent && !valid)">{{ intent ? '核对并重试原退款请求' : '审批退款至余额' }}</UiButton>
        <UiButton v-if="fact && intent?.refund_id" type="button" variant="secondary" :disabled="saving" @click="startAnother">开始另一笔退款申请</UiButton>
        <UiButton v-if="intent?.rejected_status === 400" type="button" variant="secondary" :disabled="saving" @click="reviseRejected">修改被拒绝的申请</UiButton>
      </form>
      <p v-if="intent?.refund_id && !fact">已留存退款记录 #{{ intent.refund_id }}，请核对服务端记录。</p>
      <p v-if="factLoading" role="status">正在核对退款记录…</p>
      <PageAlert v-if="factError" tone="warning" title="退款记录暂时无法核对">{{ factError }}</PageAlert>
      <section v-if="fact" class="stack" aria-label="已确认退款事实">
        <p>退款 #{{ fact.id }} · {{ formatWalletMoney(fact.amount_minor, fact.currency) }} 已退入站内余额</p>
        <p>来源订单 #{{ fact.order_id }} · 用户 #{{ fact.user_id }} · 余额流水 #{{ fact.entry_id }}</p>
        <p>工单 #{{ fact.ticket_id }} · 审批人 #{{ fact.actor_id }} · <TimeBadge :value="fact.created_at" /></p>
        <p>{{ fact.reason }}</p>
      </section>
      <UiButton v-if="intent?.refund_id" type="button" variant="secondary" :disabled="factLoading || saving" @click="verifyKnownRefund">核对退款记录与余额</UiButton>
      <section v-if="fact" aria-label="退款后余额">
        <p v-if="walletLoading" role="status">正在读取余额…</p>
        <p v-if="wallet && !walletError">余额 {{ formatWalletMoney(wallet.balance_minor, wallet.currency) }} · 已预留 {{ formatWalletMoney(wallet.reserved_minor, wallet.currency) }} · 可用 {{ formatWalletMoney(wallet.available_minor, wallet.currency) }}</p>
        <PageAlert v-else-if="walletError" tone="warning" title="当前余额未知">{{ walletError }} 已确认的退款事实仍保留，请稍后单独刷新余额。</PageAlert>
        <RouterLink :to="{ path: '/admin/users', query: { user: String(fact.user_id) } }">核对用户余额流水</RouterLink>
      </section>
      <RouterLink :to="{ path: '/admin/audit-logs', query: { action: 'order.consumption_refund', target: `order:${order.id}` } }">核对本订单退款审计</RouterLink>
    </template>
    <ConfirmDialog :open="confirmOpen" title="确认获批退款至站内余额？" :message="confirmationMessage" confirm-text="确认退款至余额" :busy="saving" tone="danger" @close="confirmOpen = false" @confirm="approve" />
  </UiSection>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { AdminOrderDetail } from '../api/client'
import { approveConsumptionRefund, fetchConsumptionRefund, type ConsumptionRefund, type ConsumptionRefundRequest } from '../api/consumptionRefunds'
import { fetchAdministrativeWallet } from '../api/walletAdjustments'
import type { WalletPage } from '../api/wallet'
import { useAppStore } from '../stores/app'
import { useRemoteResource } from '../composables/useRemoteResource'
import { newCheckoutIntent } from '../composables/checkoutOrder'
import { commerceErrorMessage } from '../utils/commerceErrors'
import { formatWalletMoney, parseWalletAmount, walletMajor } from '../utils/walletMoney'
import { isUtf8LengthInRange } from '../utils/validation'
import UiSection from './UiSection.vue'
import UiInput from './UiInput.vue'
import UiButton from './UiButton.vue'
import FormField from './FormField.vue'
import PageAlert from './PageAlert.vue'
import TimeBadge from './TimeBadge.vue'
import ConfirmDialog from './ConfirmDialog.vue'

const props = defineProps<{ order: AdminOrderDetail }>()
const emit = defineEmits<{ changed: [refund: ConsumptionRefund] }>()
const app = useAppStore()
const actor = computed(() => app.user?.id || 0)
const administrator = computed(() => Boolean(actor.value && app.isAdmin))
const eligible = computed(() => props.order.status === 'paid' && props.order.order_type !== 'topup' && Boolean(props.order.fulfilled_at && props.order.subscription_ended_at && props.order.subscription_end_reason))
interface RefundIntent {
  actor_id: number; order_id: number; user_id: number; currency: string
  request: ConsumptionRefundRequest; refund_id?: number; rejected_status?: 400
}
const scope = computed(() => `zboard:consumption-refund:${actor.value}:${props.order.id}:${props.order.user_id}:${props.order.currency}`)
const amount = ref(''), ticket = ref(''), reason = ref(''), writeError = ref('')
const saving = ref(false), blocked = ref(true), confirmOpen = ref(false)
const intent = ref<RefundIntent | null>(null), proposed = ref<RefundIntent | null>(null)
const valid = computed(() => parseWalletAmount(amount.value) !== null && /^[1-9]\d*$/.test(ticket.value) && Number.isSafeInteger(Number(ticket.value)) && isUtf8LengthInRange(reason.value.trim(), 3, 400))
const locked = computed(() => saving.value || blocked.value || !administrator.value || confirmOpen.value || Boolean(intent.value))
const confirmationMessage = computed(() => {
  const candidate = intent.value || proposed.value
  return candidate ? `将 ${formatWalletMoney(String(candidate.request.amount_minor), candidate.currency)} 退入用户 #${candidate.user_id} 的站内余额，来源订单 #${candidate.order_id}，工单 #${candidate.request.ticket_id}。请确认退款已获批；不发起渠道退款，也不修改当前新权益。` : ''
})
function requestValid(value: ConsumptionRefundRequest) {
  return value && Number.isSafeInteger(value.amount_minor) && value.amount_minor > 0 && Number.isSafeInteger(value.ticket_id) && value.ticket_id > 0 && typeof value.reason === 'string' && isUtf8LengthInRange(value.reason, 3, 400) && /^[a-f0-9]{32}$/.test(value.idempotency_key)
}
function assertFact(value: ConsumptionRefund, expected: RefundIntent) {
  if (!value || !Number.isSafeInteger(value.id) || value.id < 1 || expected.refund_id !== undefined && value.id !== expected.refund_id || !Number.isSafeInteger(value.entry_id) || value.entry_id < 1 || value.order_id !== expected.order_id || value.user_id !== expected.user_id || value.currency !== expected.currency || value.amount_minor !== String(expected.request.amount_minor) || value.ticket_id !== expected.request.ticket_id || value.actor_id !== expected.actor_id || value.reason !== expected.request.reason) throw new Error('退款响应与原申请不一致，请核对原订单和退款记录。')
  return value
}
const factResource = useRemoteResource<ConsumptionRefund | null>({ initial: () => null, fetch: async ({ signal }) => {
  const expected = intent.value
  if (!expected?.refund_id) throw new Error('尚无可核对的退款编号。')
  return assertFact(await fetchConsumptionRefund(expected.refund_id, signal), expected)
}, errorMessage: cause => commerceErrorMessage(cause, '退款记录读取失败，请保留原申请标识并重试核对。') })
const { data: fact, error: factError, loading: factLoading } = factResource
const walletResource = useRemoteResource<WalletPage | null>({ initial: () => null, fetch: ({ signal }) => fetchAdministrativeWallet(props.order.user_id, props.order.currency, 0, signal), errorMessage: '余额读取失败，请稍后重试。' })
const { data: wallet, error: walletError, loading: walletLoading } = walletResource
function requestApproval() {
  if (saving.value || blocked.value || confirmOpen.value || !administrator.value || !eligible.value || intent.value?.refund_id || !intent.value && !valid.value) return
  proposed.value = intent.value || { actor_id: actor.value, order_id: props.order.id, user_id: props.order.user_id, currency: props.order.currency, request: { amount_minor: parseWalletAmount(amount.value)!, ticket_id: Number(ticket.value), reason: reason.value.trim(), idempotency_key: newCheckoutIntent() } }
  confirmOpen.value = true
}
async function approve() {
  if (saving.value || blocked.value || !confirmOpen.value || !administrator.value || !eligible.value || !proposed.value) return
  const key = scope.value, candidate = proposed.value
  let dispatched = false
  saving.value = true; writeError.value = ''
  try {
    // A write is dispatched only after its exact retry identity is durable locally.
    delete candidate.rejected_status
    localStorage.setItem(key, JSON.stringify(candidate))
    intent.value = candidate
    dispatched = true
    const value = assertFact(await approveConsumptionRefund(candidate.order_id, candidate.request), candidate)
    candidate.refund_id = value.id
    try { localStorage.setItem(key, JSON.stringify(candidate)) } catch {
      if (key === scope.value) writeError.value = '退款已确认，但本地记录更新失败。请保存退款编号；重新打开后只能明确重试原请求核对，不能重新申请相同退款。'
    }
    if (key !== scope.value || !administrator.value) return
    factResource.replace(value)
    emit('changed', value)
    void walletResource.load()
  } catch (cause) {
    // Validation errors are an explicit refusal. Other statuses, including a
    // conflicting key, can conceal a prior committed fact and stay locked.
    if (dispatched && (cause as any)?.response?.status === 400) {
      candidate.rejected_status = 400
      try { localStorage.setItem(key, JSON.stringify(candidate)) } catch { /* Keep the original durable request if this hint cannot be saved. */ }
    }
    if (key === scope.value) writeError.value = dispatched ? commerceErrorMessage(cause, '本次退款结果暂无法确认，请保留请求标识并核对。只有明确确认后才会重试原请求。') : '本地退款标识无法可靠保存，本次未发起退款。请先检查浏览器存储。'
  } finally { if (key === scope.value) { saving.value = false; confirmOpen.value = false; proposed.value = null } }
}
async function verifyKnownRefund() {
  const key = scope.value
  if (!administrator.value || !intent.value?.refund_id) return
  if (await factResource.load() && key === scope.value) void walletResource.load()
}
function startAnother() {
  if (saving.value || !fact.value || !intent.value?.refund_id) return
  try { localStorage.removeItem(scope.value) } catch { writeError.value = '无法更新本地退款申请记录，请先保存退款编号并核对浏览器存储。'; return }
  intent.value = null; proposed.value = null; amount.value = ''; ticket.value = ''; reason.value = ''; writeError.value = ''
  factResource.reset(); walletResource.reset()
}
function reviseRejected() {
  if (saving.value || intent.value?.rejected_status !== 400 || intent.value.refund_id || !administrator.value) return
  try { localStorage.removeItem(scope.value) } catch { writeError.value = '无法更新本地申请记录，请先检查浏览器存储。'; return }
  intent.value = null; proposed.value = null; writeError.value = ''
}
watch(() => [scope.value, administrator.value], () => {
  factResource.reset(); walletResource.reset(); intent.value = null; proposed.value = null
  saving.value = false; confirmOpen.value = false; blocked.value = true
  amount.value = ''; ticket.value = ''; reason.value = ''; writeError.value = ''
  if (!administrator.value) return
  try {
    const raw = localStorage.getItem(scope.value)
    if (raw) {
      const saved: RefundIntent = JSON.parse(raw)
      if (!saved || saved.actor_id !== actor.value || saved.order_id !== props.order.id || saved.user_id !== props.order.user_id || saved.currency !== props.order.currency || !requestValid(saved.request) || saved.refund_id !== undefined && (!Number.isSafeInteger(saved.refund_id) || saved.refund_id < 1) || saved.rejected_status !== undefined && saved.rejected_status !== 400) throw new Error('invalid saved refund')
      intent.value = saved
      amount.value = walletMajor(saved.request.amount_minor); ticket.value = String(saved.request.ticket_id); reason.value = saved.request.reason
    }
    blocked.value = false
    if (intent.value?.refund_id) void verifyKnownRefund()
  } catch { writeError.value = '本地退款申请记录无法可靠读取，已停止提交。请保留记录，核对原工单与账务后处理；刷新页面不会重新发起退款。' }
}, { immediate: true })
</script>
