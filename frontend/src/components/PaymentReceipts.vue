<template>
  <UiSection :title="admin ? '收款核对' : '收款记录'" variant="outlined" class="payment-receipts">
    <template #actions><PageRefreshButton label="刷新收据" :loading="loading" :disabled="saving" @click="load" /></template>
    <div class="receipt-list" :aria-busy="loading">
      <PageAlert v-if="error" tone="danger">{{ error }}</PageAlert>
      <TableSkeleton v-if="loading && !page" label="正在核对收款" :columns="3" :rows="1" />
      <p v-if="page && !page.items.length && !error" class="receipt-empty">{{ admin ? '暂无独立收据；历史人工付款请结合订单与工单核实。' : '暂无收款记录' }}</p>
      <article v-for="receipt in page?.items || []" :key="receipt.id" class="receipt-record" :aria-label="`收据 #${receipt.id}`">
        <div class="receipt-summary">
          <strong class="receipt-amount">{{ formatWalletMoney(receipt.amount_minor, receipt.currency) }}</strong>
          <StatusBadge :tone="receipt.disposition === 'review_required' ? 'warning' : knownDisposition(receipt.disposition) ? 'success' : 'warning'">{{ label(receipt.disposition) }}</StatusBadge>
          <span class="receipt-time"><TimeBadge :value="receipt.created_at" /></span>
          <UiButton v-if="showWalletLink && (receipt.disposition === 'wallet_credit' || receipt.disposition === 'wallet_topup')" as-child variant="ghost" size="sm" class="receipt-wallet-link"><RouterLink :to="admin ? '/admin/users' : { path: '/account/wallet', query: { currency: receipt.currency } }">{{ admin ? '查看用户记录' : '余额流水' }}<UiIcon name="chevron" /></RouterLink></UiButton>
        </div>
        <details class="receipt-details"><summary>交易信息</summary><dl>
          <div><dt>收据编号</dt><dd>#{{ receipt.id }}</dd></div>
          <div v-if="admin && receipt.provider"><dt>收款渠道</dt><dd>{{ receipt.provider }}</dd></div>
          <div v-if="admin && receipt.provider_trade_no"><dt>渠道交易号</dt><dd>{{ receipt.provider_trade_no }}</dd></div>
          <div v-if="receipt.ticket_id"><dt>关联工单</dt><dd>#{{ receipt.ticket_id }}</dd></div>
          <div v-if="receipt.resolution_reason"><dt>核对依据</dt><dd>{{ receipt.resolution_reason }}</dd></div>
        </dl></details>
        <PageAlert v-if="receipt.disposition === 'review_required'" tone="warning">旧人工付款缺少渠道身份，尚不能判断本收据是否已计入原付款；款项已留存待核对，请关联工单处理。</PageAlert>
        <div v-if="admin && receipt.disposition === 'review_required' && reviewing !== receipt.id" class="receipt-review-actions"><UiButton variant="secondary" :disabled="saving" @click="reviewing = receipt.id; ticket = ''; reason = ''; decision = ''">核对这笔收据</UiButton></div>
        <form novalidate class="receipt-review-form" v-if="admin && receipt.disposition === 'review_required' && reviewing === receipt.id" @submit.prevent="resolve(receipt.id)">
          <FormField v-slot="{ controlAttrs }" label="该用户的工单编号" :name="`receipt-ticket-${receipt.id}`"><UiInput v-model="ticket" v-bind="controlAttrs" inputmode="numeric" :disabled="saving" /></FormField>
          <FormField v-slot="{ controlAttrs }" label="核对结论" :name="`receipt-decision-${receipt.id}`"><UiSelect v-model="decision" v-bind="controlAttrs" :disabled="saving" :options="[{ value: '', label: '请选择核对结论' }, { value: 'credit_wallet', label: '确认是尚未入账的新款项，转入余额' }, { value: 'already_applied', label: '确认已计入原付款，关联原履约' }]" /></FormField>
          <FormField v-slot="{ controlAttrs }" label="核对依据" :name="`receipt-reason-${receipt.id}`"><UiInput v-model="reason" v-bind="controlAttrs" maxlength="500" :disabled="saving" /></FormField>
          <div class="receipt-review-actions"><UiButton type="submit" :loading="saving" :disabled="!decision || !ticket || reason.trim().length < 3">确认结论并记账</UiButton></div>
        </form>
      </article>
      <PageAlert v-if="saveError" tone="danger">{{ saveError }}</PageAlert>
      <div v-if="page?.has_more || after" class="panel-actions"><UiButton v-if="page?.has_more" variant="secondary" :disabled="loading" @click="older">较早收据</UiButton>
      <UiButton v-if="after" variant="secondary" :disabled="loading" @click="after = 0; load()">最新收据</UiButton></div>
    </div>
  </UiSection>
</template>
<script setup lang="ts">
import { ref, watch } from 'vue'
import { useAppStore } from '../stores/app'
import { fetchPaymentReceipts, resolvePaymentReceipt, type PaymentReceiptPage } from '../api/paymentReceipts'
import { useRemoteResource } from '../composables/useRemoteResource'
import { formatWalletMoney } from '../utils/walletMoney'
import { commerceErrorMessage } from '../utils/commerceErrors'
import UiSection from './UiSection.vue'
import UiButton from './UiButton.vue'
import UiInput from './UiInput.vue'
import UiSelect from './UiSelect.vue'
import FormField from './FormField.vue'
import PageAlert from './PageAlert.vue'
import TimeBadge from './TimeBadge.vue'
import StatusBadge from './StatusBadge.vue'
import PageRefreshButton from './PageRefreshButton.vue'
import TableSkeleton from './TableSkeleton.vue'
import UiIcon from './UiIcon.vue'
const props = withDefaults(defineProps<{ orderId: number; admin?: boolean; showWalletLink?: boolean }>(), { admin: false, showWalletLink: true })
const reviewing = ref<number | null>(null)
const emit = defineEmits<{ changed: [] }>()
const app = useAppStore(), after = ref(0), ticket = ref(''), reason = ref(''), decision = ref(''), saving = ref(false), saveError = ref('')
const resource = useRemoteResource<PaymentReceiptPage | null>({ initial: () => null, fetch: ({ signal }) => fetchPaymentReceipts(props.orderId, props.admin, after.value, signal), errorMessage: '收款归属暂时无法核对，请重试。' })
const { data: page, loading, error, load } = resource
function label(value: string) { return ({ fulfilled: '已用于订单履约', wallet_topup: '充值已入余额', wallet_credit: '款项已转入余额', legacy_applied: '已关联原付款', review_required: '待人工核对' } as Record<string, string>)[value] || '未知归属，请核对' }
function knownDisposition(value: string) { return ['fulfilled', 'wallet_topup', 'wallet_credit', 'legacy_applied'].includes(value) }
function older() { after.value = page.value?.items.at(-1)?.id || 0; void load() }
async function resolve(id: number) {
  const ticketID = Number(ticket.value)
  if (saving.value || !Number.isSafeInteger(ticketID) || ticketID < 1) return
  const actor = app.user.id, order = props.orderId
  saving.value = true; saveError.value = ''
  try {
    await resolvePaymentReceipt(order, { receipt_id: id, ticket_id: ticketID, decision: decision.value, reason: reason.value.trim() })
    if (actor !== app.user.id || order !== props.orderId) return
    ticket.value = ''; reason.value = ''; decision.value = ''; emit('changed'); await load()
  } catch (cause) { if (actor === app.user.id && order === props.orderId) saveError.value = commerceErrorMessage(cause, '核对结果暂无法确认，请先刷新收据，再用原结论重试。') }
  finally { saving.value = false }
}
watch(() => [props.orderId, props.admin, app.user.id], () => { resource.reset(); reviewing.value = null; after.value = 0; ticket.value = ''; reason.value = ''; decision.value = ''; saveError.value = ''; if (props.orderId && app.user.id) void load() }, { immediate: true })
</script>

<style scoped>
.payment-receipts { container-type: inline-size; }
.receipt-list { display: grid; gap: 16px; min-width: 0; }
.receipt-record { display: grid; gap: 12px; min-width: 0; }
.receipt-record + .receipt-record { border-top: 1px solid var(--border); padding-top: 16px; }
.receipt-summary { display: flex; align-items: center; flex-wrap: wrap; gap: 8px 16px; min-width: 0; }
.receipt-amount { font-size: 14px; font-weight: 600; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
.receipt-time { color: var(--muted); font-size: 12px; margin-left: auto; }
.receipt-details { font-size: 12px; min-width: 0; }
.receipt-details summary { color: var(--muted); cursor: pointer; width: fit-content; }
.receipt-details dl { display: grid; gap: 12px; margin: 14px 0 0; }
.receipt-details dl > div { display: grid; grid-template-columns: 100px minmax(0,1fr); gap: 12px; }
.receipt-details dt { color: var(--muted); }
.receipt-details dd { margin: 0; overflow-wrap: anywhere; }
.receipt-empty { margin: 0; color: var(--muted); font-size: 13px; }
.receipt-review-form { max-width: 480px; display: grid; gap: 16px; }
.receipt-review-actions { display: flex; flex-wrap: wrap; gap: 8px; }
@container (max-width: 420px) {
  .receipt-summary { gap: 8px 12px; }
  .receipt-time { flex: 1 1 100%; margin: 0; }
  .receipt-details dl > div { grid-template-columns: 80px minmax(0,1fr); }
}
</style>
