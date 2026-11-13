<template>
  <section class="admin-wallet" aria-label="用户余额">
    <div class="wallet-toolbar">
      <PopoverRoot v-model:open="currencyOpen">
        <PopoverTrigger as-child><UiButton variant="ghost" size="sm" :disabled="saving || changeSaving" aria-label="切换余额币种">{{ currency }}<UiIcon name="chevron" class="wallet-currency-chevron" /></UiButton></PopoverTrigger>
        <PopoverPortal><PopoverContent as-child align="start" :side-offset="6" :collision-padding="16">
          <form novalidate class="ui-popover-content wallet-currency-editor" @submit.prevent="applyCurrency">
            <FormField v-slot="{ controlAttrs }" label="币种" name="admin-wallet-currency"><UiInput v-model="currencyInput" v-bind="controlAttrs" maxlength="3" autocomplete="off" :disabled="saving || changeSaving" /></FormField>
            <UiButton type="submit" size="sm" :disabled="saving || changeSaving">切换</UiButton>
          </form>
        </PopoverContent></PopoverPortal>
      </PopoverRoot>
      <div class="wallet-toolbar-actions"><PageRefreshButton label="刷新余额" :loading="loading" :disabled="saving || changeSaving" @click="load" /><UiButton data-test="direct-debit-open" variant="secondary" size="sm" :disabled="!data || saving || changeSaving || !actor" @click="openBalanceChange('debit')"><UiIcon name="minus" />扣减余额</UiButton><UiButton data-test="credit-open" size="sm" :disabled="!data || saving || changeSaving || !actor" @click="openBalanceChange('credit')"><UiIcon name="plus" />增加余额</UiButton></div>
    </div>
    <PageAlert v-if="error" tone="danger">{{ error }}</PageAlert>
    <p v-if="loading && !data" role="status">正在加载余额…</p>
    <div v-if="data" :key="`${actor}:${userId}:${currency}`" class="wallet-body">
      <BalanceSummary :balance="data.wallet" :show-total="false" />
      <UiSection title="余额流水">
        <DataTable v-if="data.wallet.entries.length" caption="余额流水" :min-width="480">
          <thead><tr><th class="table-primary-column">原因</th><th class="numeric-column">变动金额</th><th class="numeric-column" data-column-priority="2">变动后余额</th><th data-column-priority="2">时间</th></tr></thead>
          <tbody><tr v-for="entry in data.wallet.entries" :key="entry.id"><td class="table-primary-column"><TableText :value="entry.reason" /></td><td class="numeric-column" :aria-label="money(entry.balance_delta)">{{ delta(entry.balance_delta) }}</td><td class="numeric-column" data-column-priority="2"><BalanceValue :amount="entry.balance_after" :currency="currency" :show-currency="false" /></td><td data-column-priority="2"><TimeBadge :value="entry.created_at" /></td></tr></tbody>
        </DataTable>
        <EmptyState v-else icon="billing" title="暂无余额流水" description="" />
        <div v-if="data.wallet.has_more || ledgerAfter" class="wallet-pagination"><UiButton v-if="data.wallet.has_more" variant="secondary" size="sm" :disabled="saving || changeSaving || loading" @click="ledgerAfter = data!.wallet.entries.at(-1)?.id || 0; load()">较早流水</UiButton><UiButton v-if="ledgerAfter" variant="ghost" size="sm" :disabled="saving || changeSaving || loading" @click="ledgerAfter = 0; load()">最新流水</UiButton></div>
      </UiSection>
      <CollapsibleRoot v-model:open="payoutOpen" class="wallet-payouts">
        <CollapsibleTrigger as-child><UiButton variant="ghost" size="sm" data-test="payout-toggle"><UiIcon name="chevron" :class="{ 'wallet-payout-expanded': payoutOpen }" />人工出款</UiButton></CollapsibleTrigger>
        <CollapsibleContent class="wallet-payout-body">
          <form novalidate data-test="debit" class="form-grid" @submit.prevent="debit">
            <FormField v-slot="{ controlAttrs }" :label="`扣减金额（${currency}）`" name="admin-wallet-amount"><UiInput v-model="amount" v-bind="controlAttrs" inputmode="decimal" :disabled="saving || Boolean(debitIntent)" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="工单编号" name="admin-wallet-ticket"><UiInput v-model="ticket" v-bind="controlAttrs" inputmode="text" :disabled="saving || Boolean(debitIntent)" /></FormField>
            <FormField v-slot="{ controlAttrs }" label="扣减原因（用户可见）" name="admin-wallet-reason" full><UiInput v-model="reason" v-bind="controlAttrs" maxlength="400" :disabled="saving || Boolean(debitIntent)" /></FormField>
            <p class="wallet-consequence">扣减余额不会自动转账。</p>
            <PageAlert v-if="writeError" class="wallet-form-wide" tone="danger">{{ writeError }}</PageAlert>
            <p v-if="debitIntent && !debitIntent.result" class="wallet-form-wide" role="status">扣减结果待确认，请核对流水或重试原请求。</p>
            <div class="wallet-form-actions"><UiButton type="submit" :loading="saving" :disabled="changeSaving || debitBlocked || Boolean(debitIntent?.result) || (!debitIntent && (!validDebit || !actor))">{{ debitIntent && !debitIntent.result ? '重试原扣减请求' : '确认扣减余额' }}</UiButton><UiButton v-if="debitIntent?.result" type="button" variant="secondary" :disabled="saving" @click="newDebit">开始另一笔扣减</UiButton></div>
          </form>
          <UiSection title="出款记录">
            <DataTable v-if="data.adjustments.items.length" caption="人工出款记录" :min-width="480">
              <thead><tr><th class="table-primary-column">原因</th><th class="numeric-column">金额</th><th>状态</th><th class="table-action-column"><span class="sr-only">操作</span></th></tr></thead>
              <tbody><tr v-for="item in data.adjustments.items" :key="item.id"><td class="table-primary-column"><TableText :value="item.reason" /></td><td class="numeric-column" :aria-label="money(item.amount_minor)">{{ walletMajor(item.amount_minor) }}</td><td><StatusBadge :tone="item.reversal_entry_id ? 'success' : item.external_status === 'failed' ? 'danger' : item.external_status === 'succeeded' ? 'success' : 'warning'">{{ item.reversal_entry_id ? '已恢复余额' : label(item.external_status) }}</StatusBadge></td><td class="table-action-column"><UiButton variant="ghost" size="sm" :disabled="saving || changeSaving" @click="openCase(item.id)">核对这笔出款</UiButton></td></tr></tbody>
            </DataTable>
            <EmptyState v-else icon="billing" title="暂无出款记录" description="" />
            <div v-if="data.adjustments.has_more || after" class="wallet-pagination"><UiButton v-if="data.adjustments.has_more" variant="secondary" size="sm" :disabled="saving || loading" @click="after = data!.adjustments.items.at(-1)?.id || 0; load()">较早出款记录</UiButton><UiButton v-if="after" variant="ghost" size="sm" :disabled="saving || loading" @click="after = 0; load()">最新出款记录</UiButton></div>
          </UiSection>
          <section v-if="selected" class="wallet-payout-body" aria-label="人工出款核对">
            <div class="wallet-case-heading"><strong>{{ money(selected.amount_minor) }}</strong><span>工单 {{ ticketReference(selected.ticket_id) }}</span></div>
            <p v-if="selected.reversal_entry_id">{{ selected.reversal_reason }}</p>
            <p v-for="result in selected.results || []" :key="result.id">{{ label(result.outcome) }} · {{ result.reference || result.note }} · <TimeBadge :value="result.occurred_at" /></p>
            <form novalidate data-test="payout" class="form-grid" @submit.prevent="report">
              <FormField v-slot="{ controlAttrs }" label="核对结果" name="admin-wallet-outcome" full><UiSelect v-model="outcome" v-bind="controlAttrs" :disabled="saving || Boolean(reportIntent)" :options="[{value:'unknown',label:'尚未确定'}, {value:'succeeded',label:'已确认出款成功'}, {value:'failed',label:'已确认出款失败'}]" /></FormField>
              <FormField v-slot="{ controlAttrs }" label="外部流水（仅管理员可见）" name="admin-wallet-reference"><UiInput v-model="reference" v-bind="controlAttrs" maxlength="128" :disabled="saving || Boolean(reportIntent)" /></FormField>
              <FormField v-slot="{ controlAttrs }" label="核对依据（仅管理员可见）" name="admin-wallet-note"><UiInput v-model="note" v-bind="controlAttrs" maxlength="500" :disabled="saving || Boolean(reportIntent)" /></FormField>
              <div class="wallet-form-actions"><UiButton type="submit" :loading="saving" :disabled="changeSaving || reportBlocked || Boolean(reportIntent?.result)">{{ reportIntent && !reportIntent.result ? '重试原结果记录' : '记录核对结果' }}</UiButton><UiButton v-if="reportIntent?.result" type="button" variant="secondary" :disabled="saving" @click="newReport">添加核对记录</UiButton></div>
            </form>
            <form novalidate v-if="selected.external_status === 'failed' && !selected.reversal_entry_id" data-test="reverse" class="form-grid" @submit.prevent="reverse">
              <FormField v-slot="{ controlAttrs }" label="恢复余额原因（用户可见）" name="admin-wallet-reversal" full><UiInput v-model="reversalReason" v-bind="controlAttrs" maxlength="400" :disabled="saving" /></FormField>
              <div class="wallet-form-actions"><UiButton type="submit" :loading="saving" :disabled="changeSaving || reversalReason.trim().length < 3">确认失败并恢复本笔扣减</UiButton></div>
            </form>
          </section>
        </CollapsibleContent>
      </CollapsibleRoot>
    </div>
    <ModalDialog :open="changeOpen" :title="changeLabel + '余额'" :description="userLabel" size="sm" :busy="changeSaving" :dirty="!changeIntent && Boolean(changeAmount || changeReason)" initial-focus-selector="#admin-wallet-change-amount" :return-focus-selector="changeDirection === 'credit' ? '[data-test=\'credit-open\']' : '[data-test=\'direct-debit-open\']'" @close="changeOpen = false">
      <form id="admin-wallet-change-form" data-test="balance-change" class="wallet-change-form" novalidate @submit.prevent="changeBalance">
        <FormField v-slot="{ controlAttrs }" :label="`金额（${currency}）`" name="admin-wallet-change-amount" required :error="changeErrors.amount"><UiInput v-model="changeAmount" v-bind="controlAttrs" inputmode="decimal" autocomplete="off" placeholder="0.00" :disabled="changeSaving || Boolean(changeIntent)" /></FormField>
        <FormField v-slot="{ controlAttrs }" :label="changeLabel + '原因（用户可见）'" name="admin-wallet-change-reason" required :error="changeErrors.reason"><UiInput v-model="changeReason" v-bind="controlAttrs" maxlength="400" autocomplete="off" :placeholder="changeDirection === 'credit' ? '例如：活动赠送' : '例如：纠正误加余额'" :disabled="changeSaving || Boolean(changeIntent)" /></FormField>
        <PageAlert v-if="changeError" tone="danger">{{ changeError }}</PageAlert>
      </form>
      <template #footer="{ requestClose }"><UiButton variant="ghost" :disabled="changeSaving" @click="requestClose">取消</UiButton><UiButton type="submit" form="admin-wallet-change-form" :variant="changeDirection === 'debit' ? 'danger' : 'primary'" :loading="changeSaving" :disabled="changeBlocked || !actor">{{ changeIntent && !changeIntent.result ? '重试' + changeLabel + '余额' : '确认' + changeLabel }}</UiButton></template>
    </ModalDialog>
  </section>
</template>
<script setup lang="ts">
import { ticketReference, parseBusinessReference } from '../utils/businessReference'
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { CollapsibleContent, CollapsibleRoot, CollapsibleTrigger, PopoverContent, PopoverPortal, PopoverRoot, PopoverTrigger } from 'reka-ui'
import ModalDialog from './ModalDialog.vue'
import DataTable from './DataTable.vue'
import TableText from './TableText.vue'
import EmptyState from './EmptyState.vue'
import BalanceValue from './BalanceValue.vue'
import StatusBadge from './StatusBadge.vue'
import PageRefreshButton from './PageRefreshButton.vue'
import UiIcon from './UiIcon.vue'
import { notify } from '../utils/feedback'
import { useAppStore } from '../stores/app'
import { platformDefaultCurrency } from '../utils/platformCurrency'
import { useRemoteResource } from '../composables/useRemoteResource'
import { newCheckoutIntent } from '../composables/checkoutOrder'
import { commerceErrorMessage } from '../utils/commerceErrors'
import BalanceSummary from './BalanceSummary.vue'
import { formatWalletMoney, parseWalletAmount, walletMajor } from '../utils/walletMoney'
import { creditWallet, directlyDebitWallet, type WalletCredit, debitWallet, fetchAdministrativeWallet, fetchWalletAdjustments, fetchWalletAdjustment, recordWalletPayout, reverseWalletAdjustment, type WalletAdjustment, type WalletDebit, type WalletPayout } from '../api/walletAdjustments'
import UiSection from './UiSection.vue'
import UiInput from './UiInput.vue'
import UiSelect from './UiSelect.vue'
import UiButton from './UiButton.vue'
import FormField from './FormField.vue'
import PageAlert from './PageAlert.vue'
import TimeBadge from './TimeBadge.vue'
const props = defineProps<{ userId: number; initialCurrency?: string; userLabel?: string }>(), app = useAppStore()
const emit = defineEmits<{ changed: [] }>()
const currencyOpen = ref(false), payoutOpen = ref(false)
const actor = computed(() => app.user?.id || 0)
const initialCurrency = /^[A-Z]{3,16}$/.test(props.initialCurrency || '') ? props.initialCurrency! : platformDefaultCurrency(app.publicConfigs)
const currency = ref(initialCurrency), currencyInput = ref(initialCurrency), after = ref(0), ledgerAfter = ref(0)
const amount = ref(''), ticket = ref(''), reason = ref(''), outcome = ref('unknown'), reference = ref(''), note = ref(''), reversalReason = ref('')
const debitBlocked = ref(false), reportBlocked = ref(false)
const saving = ref(false), writeError = ref(''), selected = ref<WalletAdjustment | null>(null)
interface Intent<T> { request: T; result?: number }
const debitIntent = ref<Intent<WalletDebit> | null>(null), reportIntent = ref<Intent<WalletPayout> | null>(null)
const scope = () => `zboard:wallet-adjustment:${actor.value}:${props.userId}:${currency.value}`
const reportScope = () => `${scope()}:payout:${selected.value?.id}`
function restore<T>(key: string): Intent<T> | null { const raw = localStorage.getItem(key); if (!raw) return null; const value = JSON.parse(raw); if (!value?.request?.idempotency_key) throw new Error('扣减请求记录不完整，请先核对工单和账务记录。'); return value }
function persist<T>(key: string, value: Intent<T>) { localStorage.setItem(key, JSON.stringify(value)) }
const resource = useRemoteResource({ initial: () => null as { wallet: Awaited<ReturnType<typeof fetchAdministrativeWallet>>; adjustments: Awaited<ReturnType<typeof fetchWalletAdjustments>> } | null, fetch: async ({ signal }) => { const [wallet, adjustments] = await Promise.all([fetchAdministrativeWallet(props.userId, currency.value, ledgerAfter.value, signal), fetchWalletAdjustments(props.userId, currency.value, after.value, signal)]); return { wallet, adjustments } }, errorMessage: '余额暂时无法核对，请刷新。' })
const { data, loading, error, load } = resource
const validDebit = computed(() => parseWalletAmount(amount.value) !== null && parseBusinessReference(ticket.value, 'T') !== undefined && reason.value.trim().length >= 3)
function delta(value: string) { return /^-?\d+$/.test(value) ? `${BigInt(value) > 0n ? '+' : ''}${walletMajor(value)}` : '-' }
function money(value: string) { return formatWalletMoney(value, currency.value) }
function label(value: string) { return ({ pending: '待核对外部出款', unknown: '外部结果未知', succeeded: '人工确认出款成功', failed: '人工确认出款失败' } as Record<string,string>)[value] || '结果未知' }
function applyCurrency() { if (saving.value || changeSaving.value) return; const next = currencyInput.value.trim().toUpperCase(); if (!/^[A-Z]{3,16}$/.test(next)) { writeError.value = '请输入有效币种。'; return }; if (currency.value === next) void load(); else currency.value = next; currencyOpen.value = false }
async function act(work: () => Promise<void>) { if (saving.value || changeSaving.value || !actor.value) return; const original = scope(); saving.value = true; writeError.value = ''; try { await work() } catch (cause) { if (original === scope()) writeError.value = commerceErrorMessage(cause, '操作结果暂无法确认，请先核对记录，再明确重试原请求。') } finally { saving.value = false } }
async function openCase(id: number) { const original = scope(); await act(async () => { const value = await fetchWalletAdjustment(id); if (original !== scope()) return; selected.value = value; reportBlocked.value = true; reportIntent.value = restore<WalletPayout>(reportScope()); reportBlocked.value = false; outcome.value = reportIntent.value?.request.outcome || 'unknown'; reference.value = reportIntent.value?.request.reference || ''; note.value = reportIntent.value?.request.note || ''; reversalReason.value = '' }) }
async function debit() { if (debitBlocked.value) return; await act(async () => { if (!debitIntent.value && !validDebit.value) return; const key = scope(); const intent = debitIntent.value || { request: { user_id: props.userId, currency: currency.value, amount_minor: parseWalletAmount(amount.value)!, ticket_id: parseBusinessReference(ticket.value, 'T')!, reason: reason.value.trim(), idempotency_key: newCheckoutIntent() } }; if (intent.result) return; persist(key, intent); debitIntent.value = intent; const value = await debitWallet(intent.request); intent.result = value.id; persist(key, intent); if (key !== scope()) return; selected.value = value; reportIntent.value = null; await load(); emit('changed') }) }
function newDebit() { if (!debitIntent.value?.result) return; localStorage.removeItem(scope()); debitIntent.value = null; amount.value = ''; ticket.value = ''; reason.value = '' }
async function report() { if (reportBlocked.value) return; await act(async () => { if (!selected.value) return; const original = scope(), id = selected.value.id, key = reportScope(); const intent = reportIntent.value || { request: { outcome: outcome.value, reference: reference.value.trim(), note: note.value.trim(), occurred_at: new Date().toISOString(), idempotency_key: newCheckoutIntent() } }; if (intent.result) return; persist(key, intent); reportIntent.value = intent; const value = await recordWalletPayout(id, intent.request); intent.result = value.id; persist(key, intent); if (original !== scope()) return; const detail = await fetchWalletAdjustment(id); if (original !== scope()) return; selected.value = detail; await load(); emit('changed') }) }
function newReport() { if (!reportIntent.value?.result) return; localStorage.removeItem(reportScope()); reportIntent.value = null; outcome.value = 'unknown'; reference.value = ''; note.value = '' }
async function reverse() { await act(async () => { if (!selected.value || reversalReason.value.trim().length < 3) return; const original = scope(); const value = await reverseWalletAdjustment(selected.value.id, reversalReason.value.trim()); if (original !== scope()) return; selected.value = value; reversalReason.value = ''; await load(); emit('changed') }) }
const changeDirection = ref<'credit' | 'debit'>('credit')
const changeLabel = computed(() => changeDirection.value === 'credit' ? '增加' : '扣减')
const changeOpen = ref(false), changeSaving = ref(false), changeBlocked = ref(false)
const changeAmount = ref(''), changeReason = ref(''), changeError = ref('')
const changeErrors = ref({ amount: '', reason: '' })
const changeIntent = ref<Intent<WalletCredit> | null>(null)
let changeGeneration = 0
const changeScope = () => `${scope()}:${changeDirection.value === 'credit' ? 'credit' : 'direct-debit'}`
function restoreBalanceChange() {
  changeBlocked.value = true
  try {
    const intent = restore<WalletCredit>(changeScope())
    if (intent) {
      const request = intent.request
      if (request.user_id !== props.userId || request.currency !== currency.value || !Number.isSafeInteger(request.amount_minor) || request.amount_minor <= 0 || typeof request.reason !== 'string' || (!request.reason.trim() || [...request.reason].length > 400) || !/^[a-zA-Z0-9_-]{8,80}$/.test(request.idempotency_key) || (intent.result !== undefined && (!Number.isSafeInteger(intent.result) || intent.result <= 0))) throw new Error('invalid credit intent')
      changeIntent.value = intent
      changeAmount.value = walletMajor(request.amount_minor); changeReason.value = request.reason
    }
    changeBlocked.value = false
    return intent
  } catch { changeError.value = `${changeLabel.value}余额记录无法读取，请先核对余额流水。`; return null }
}
function openBalanceChange(direction: 'credit' | 'debit') {
  if (saving.value || changeSaving.value || !actor.value) return
  changeDirection.value = direction
  changeIntent.value = null; changeAmount.value = ''; changeReason.value = ''; changeError.value = ''
  const restored = restoreBalanceChange()
  changeErrors.value = { amount: '', reason: '' }
  if (restored?.result) {
    localStorage.removeItem(changeScope()); changeIntent.value = null; changeAmount.value = ''; changeReason.value = ''
  }
  changeOpen.value = true
}
async function changeBalance() {
  if (changeBlocked.value || saving.value || changeSaving.value || !actor.value || !changeOpen.value) return
  const generation = changeGeneration, key = changeScope(), direction = changeDirection.value, label = changeLabel.value
  const minor = parseWalletAmount(changeAmount.value), text = changeReason.value.trim()
  changeErrors.value = { amount: minor === null ? '请输入正金额，最多两位小数。' : '', reason: !text ? `请填写${changeLabel.value}原因。` : [...text].length > 400 ? '原因最多 400 字。' : '' }
  if (!changeIntent.value && (changeErrors.value.amount || changeErrors.value.reason)) {
    await nextTick(); document.getElementById(changeErrors.value.amount ? 'admin-wallet-change-amount' : 'admin-wallet-change-reason')?.focus(); return
  }
  changeSaving.value = true; changeError.value = ''
  try {
    const intent = changeIntent.value || { request: { user_id: props.userId, currency: currency.value, amount_minor: minor!, reason: text, idempotency_key: newCheckoutIntent() } }
    if (intent.result) return
    persist(key, intent); changeIntent.value = intent
    const entry = await (direction === 'credit' ? creditWallet : directlyDebitWallet)(intent.request)
    if (!Number.isSafeInteger(entry.id) || entry.id <= 0 || entry.kind !== (direction === 'credit' ? 'admin_credit' : 'admin_debit') || entry.balance_delta !== String((direction === 'credit' ? 1 : -1) * intent.request.amount_minor) || entry.reserved_delta !== '0') throw new Error(`${label}结果暂无法确认，请核对流水或重试原请求。`)
    intent.result = entry.id; persist(key, intent)
    if (generation !== changeGeneration) return
    changeOpen.value = false; ledgerAfter.value = 0
    notify(`余额已${label}`, '', 'success'); emit('changed'); await load()
  } catch (cause) {
    if (generation === changeGeneration) {
      changeError.value = commerceErrorMessage(cause, `${label}结果待确认，请核对流水或重试原请求。`)
      // A definitive validation rejection made no ledger change. Keep fields editable.
      if ((cause as { response?: { status?: number } })?.response?.status === 400 && !changeIntent.value?.result) {
        localStorage.removeItem(key); changeIntent.value = null
      }
    }
  } finally { if (generation === changeGeneration) changeSaving.value = false }
}
watch(() => [props.userId, actor.value, currency.value, app.token], () => {
  changeGeneration++; payoutOpen.value = false; changeOpen.value = false; changeSaving.value = false; changeIntent.value = null
  changeAmount.value = ''; changeReason.value = ''; changeError.value = ''; changeErrors.value = { amount: '', reason: '' }
  changeBlocked.value = false
  if (props.userId && actor.value) restoreBalanceChange()
}, { immediate: true, flush: 'sync' })
onBeforeUnmount(() => { changeGeneration++ })
watch(() => [props.userId, actor.value, currency.value], () => { resource.reset(); after.value = 0; ledgerAfter.value = 0; selected.value = null; reportIntent.value = null; debitIntent.value = null; amount.value = ''; ticket.value = ''; reason.value = ''; outcome.value = 'unknown'; reference.value = ''; note.value = ''; reversalReason.value = ''; writeError.value = ''; debitBlocked.value = true; reportBlocked.value = false; if (props.userId && actor.value) { try { debitIntent.value = restore<WalletDebit>(scope()); if (debitIntent.value) { if (debitIntent.value.request.user_id !== props.userId || debitIntent.value.request.currency !== currency.value) throw new Error('扣减草稿与当前账户或币种不符，请先核对原记录。'); amount.value = walletMajor(debitIntent.value.request.amount_minor); ticket.value = ticketReference(debitIntent.value.request.ticket_id); reason.value = debitIntent.value.request.reason; if (!debitIntent.value.result) payoutOpen.value = true }; debitBlocked.value = false } catch (cause) { writeError.value = String(cause); return }; void load() } }, { immediate: true })
</script>

<style scoped>
.admin-wallet, .wallet-body, .wallet-payout-body { display: grid; gap: 24px; min-width: 0; }
.wallet-toolbar, .wallet-toolbar-actions, .wallet-form-actions, .wallet-pagination, .wallet-case-heading { display: flex; align-items: center; gap: 8px; }
.wallet-toolbar { justify-content: space-between; flex-wrap: wrap; }
.wallet-toolbar-actions { flex-wrap: wrap; }
.wallet-currency-chevron { transform: rotate(90deg); }
.wallet-form-actions, .wallet-pagination { flex-wrap: wrap; }
.wallet-form-actions, .wallet-form-wide, .wallet-consequence { grid-column: 1 / -1; }
.wallet-case-heading { justify-content: space-between; }
.wallet-case-heading span, .wallet-consequence { color: var(--muted-foreground); font-size: 12px; }
.wallet-consequence { margin: 0; }
.wallet-change-form { display: grid; gap: 16px; }
.wallet-payouts { border-top: 1px solid var(--border); padding-top: 16px; }
.wallet-payout-expanded { transform: rotate(90deg); }
.wallet-payout-body { margin-top: 16px; }
.wallet-currency-editor { display: grid; gap: 12px; width: min(220px, calc(100vw - 32px)); box-sizing: border-box; }
.wallet-currency-editor > button { justify-self: end; }
.admin-wallet :deep(.balance-summary dd) { font-size: 24px; }
@media (max-width: 600px) { .wallet-form-actions { justify-content: stretch; } .wallet-form-actions > button { flex: 1; } }
</style>
