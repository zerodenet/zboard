<template>
  <UiSection title="渠道收款归属" description="渠道收款与订单权益分别核对；重复回调不会重复入账。">
    <PageAlert v-if="error" tone="danger">{{ error }}</PageAlert>
    <p v-if="loading" role="status">正在核对收款…</p>
    <p v-if="page && !page.items.length">暂无独立收据。历史人工付款请结合订单与工单核实。</p>
    <div v-for="receipt in page?.items || []" :key="receipt.id" class="stack">
      <p>收据 #{{ receipt.id }} · {{ formatWalletMoney(receipt.amount_minor, receipt.currency) }} · {{ label(receipt.disposition) }} · <TimeBadge :value="receipt.created_at" /></p>
      <p v-if="admin && receipt.provider_trade_no">渠道身份 {{ receipt.provider }} · 渠道交易号 {{ receipt.provider_trade_no }}</p>
      <RouterLink v-if="receipt.disposition === 'wallet_credit' || receipt.disposition === 'wallet_topup'" :to="admin ? '/admin/users' : { path: '/account/wallet', query: { currency: receipt.currency } }">{{ admin ? '查看用户记录' : '查看余额流水' }}</RouterLink>
      <PageAlert v-if="receipt.disposition === 'review_required'" tone="warning">旧人工付款缺少渠道身份，尚不能判断本收据是否已计入原付款；款项已留存待核对，请关联工单处理。</PageAlert>
      <p v-if="receipt.ticket_id">关联工单 #{{ receipt.ticket_id }} · {{ receipt.resolution_reason }}</p>
      <UiButton v-if="admin && receipt.disposition === 'review_required' && reviewing !== receipt.id" variant="secondary" :disabled="saving" @click="reviewing = receipt.id; ticket = ''; reason = ''; decision = ''">核对这笔收据</UiButton>
      <form v-if="admin && receipt.disposition === 'review_required' && reviewing === receipt.id" @submit.prevent="resolve(receipt.id)">
        <FormField v-slot="{ controlAttrs }" label="该用户的工单编号" :name="`receipt-ticket-${receipt.id}`"><UiInput v-model="ticket" v-bind="controlAttrs" inputmode="numeric" :disabled="saving" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="核对结论" :name="`receipt-decision-${receipt.id}`"><UiSelect v-model="decision" v-bind="controlAttrs" :disabled="saving" :options="[{ value: '', label: '请选择核对结论' }, { value: 'credit_wallet', label: '确认是尚未入账的新款项，转入余额' }, { value: 'already_applied', label: '确认已计入原付款，关联原履约' }]" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="核对依据" :name="`receipt-reason-${receipt.id}`"><UiInput v-model="reason" v-bind="controlAttrs" maxlength="500" :disabled="saving" /></FormField>
        <UiButton type="submit" :loading="saving" :disabled="!decision || !ticket || reason.trim().length < 3">确认结论并记账</UiButton>
      </form>
    </div>
    <PageAlert v-if="saveError" tone="danger">{{ saveError }}</PageAlert>
    <UiButton variant="secondary" :disabled="loading || saving" @click="load">刷新收据</UiButton>
    <UiButton v-if="page?.has_more" variant="secondary" :disabled="loading" @click="older">较早收据</UiButton>
    <UiButton v-if="after" variant="secondary" :disabled="loading" @click="after = 0; load()">最新收据</UiButton>
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
const props = withDefaults(defineProps<{ orderId: number; admin?: boolean }>(), { admin: false })
const reviewing = ref<number | null>(null)
const emit = defineEmits<{ changed: [] }>()
const app = useAppStore(), after = ref(0), ticket = ref(''), reason = ref(''), decision = ref(''), saving = ref(false), saveError = ref('')
const resource = useRemoteResource<PaymentReceiptPage | null>({ initial: () => null, fetch: ({ signal }) => fetchPaymentReceipts(props.orderId, props.admin, after.value, signal), errorMessage: '收款归属暂时无法核对，请重试。' })
const { data: page, loading, error, load } = resource
function label(value: string) { return ({ fulfilled: '已用于订单履约', wallet_topup: '充值已入余额', wallet_credit: '款项已转入余额', legacy_applied: '已关联原付款', review_required: '待人工核对' } as Record<string, string>)[value] || '未知归属，请核对' }
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
