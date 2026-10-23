<template>
  <UiSection title="余额与人工出款" description="特殊出款通过工单人工处理。扣减余额不会自动转账；外部出款结果需要另行核对。">
    <FormField v-slot="{ controlAttrs }" label="币种" name="admin-wallet-currency"><UiInput v-model="currencyInput" v-bind="controlAttrs" maxlength="16" :disabled="saving" /></FormField>
    <UiButton variant="secondary" :disabled="saving || loading" @click="applyCurrency">核对余额</UiButton>
    <PageAlert v-if="error || writeError" tone="danger">{{ error || writeError }}</PageAlert>
    <p v-if="loading" role="status">正在核对余额与扣减记录…</p>
    <div v-if="data" :key="`${actor}:${userId}:${currency}`" class="stack">
      <p>余额 {{ money(data.wallet.balance_minor) }} · 已预留 {{ money(data.wallet.reserved_minor) }} · 可扣减 {{ money(data.wallet.available_minor) }}</p>
      <form data-test="debit" @submit.prevent="debit">
        <FormField v-slot="{ controlAttrs }" label="扣减金额" name="admin-wallet-amount"><UiInput v-model="amount" v-bind="controlAttrs" inputmode="decimal" :disabled="saving || Boolean(debitIntent)" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="该用户的工单编号" name="admin-wallet-ticket"><UiInput v-model="ticket" v-bind="controlAttrs" inputmode="numeric" :disabled="saving || Boolean(debitIntent)" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="扣减原因（用户可见）" name="admin-wallet-reason"><UiInput v-model="reason" v-bind="controlAttrs" maxlength="400" :disabled="saving || Boolean(debitIntent)" /></FormField>
        <p v-if="debitIntent && !debitIntent.result">原扣减请求尚未确认，请先刷新记录或重试原请求；不会自动重试。</p>
        <UiButton type="submit" :loading="saving" :disabled="debitBlocked || Boolean(debitIntent?.result) || (!debitIntent && (!validDebit || !actor))">{{ debitIntent && !debitIntent.result ? '重试原扣减请求' : '确认扣减余额' }}</UiButton>
        <UiButton v-if="debitIntent?.result" type="button" variant="secondary" :disabled="saving" @click="newDebit">开始另一笔扣减</UiButton>
      </form>
      <UiButton variant="secondary" :disabled="saving || loading" @click="load">刷新记录</UiButton>
      <div v-for="item in data.adjustments.items" :key="item.id" class="stack">
        <p>扣减 #{{ item.id }} · {{ money(item.amount_minor) }} · 工单 #{{ item.ticket_id }} · 操作人 #{{ item.actor_id }} · {{ label(item.external_status) }}{{ item.reversal_entry_id ? ' · 余额已恢复' : '' }}</p>
        <p>{{ item.reason }}</p>
        <UiButton variant="secondary" :disabled="saving" @click="openCase(item.id)">核对这笔出款</UiButton>
      </div>
      <UiButton v-if="data.adjustments.has_more" variant="secondary" :disabled="saving || loading" @click="after = data!.adjustments.items.at(-1)?.id || 0; load()">较早扣减记录</UiButton>
      <UiButton v-if="after" variant="secondary" :disabled="saving || loading" @click="after = 0; load()">最新扣减记录</UiButton>
      <details><summary>本页余额流水</summary><p v-for="entry in data.wallet.entries" :key="entry.id">#{{ entry.id }} · {{ money(entry.balance_delta) }} · {{ entry.reason }} · <TimeBadge :value="entry.created_at" /></p>
        <UiButton v-if="data.wallet.has_more" variant="secondary" :disabled="saving || loading" @click="ledgerAfter = data!.wallet.entries.at(-1)?.id || 0; load()">较早余额流水</UiButton>
        <UiButton v-if="ledgerAfter" variant="secondary" :disabled="saving || loading" @click="ledgerAfter = 0; load()">最新余额流水</UiButton>
      </details>
    </div>
    <section v-if="selected" class="stack" aria-label="人工出款核对">
      <p>扣减 #{{ selected.id }} · {{ money(selected.amount_minor) }} · {{ label(selected.external_status) }} · 工单 #{{ selected.ticket_id }}</p>
      <p v-if="selected.reversal_entry_id">余额已恢复，账目 #{{ selected.reversal_entry_id }} · {{ selected.reversal_reason }}</p>
      <p v-for="result in selected.results || []" :key="result.id">{{ label(result.outcome) }} · {{ result.reference }} · {{ result.note }} · 操作人 #{{ result.actor_id }} · <TimeBadge :value="result.occurred_at" /></p>
      <form data-test="payout" @submit.prevent="report">
        <FormField v-slot="{ controlAttrs }" label="人工核对结果" name="admin-wallet-outcome"><UiSelect v-model="outcome" v-bind="controlAttrs" :disabled="saving || Boolean(reportIntent)" :options="[{value:'unknown',label:'尚未确定'}, {value:'succeeded',label:'已确认出款成功'}, {value:'failed',label:'已确认出款失败'}]" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="外部流水引用（仅管理员可见）" name="admin-wallet-reference"><UiInput v-model="reference" v-bind="controlAttrs" maxlength="128" :disabled="saving || Boolean(reportIntent)" /></FormField>
        <FormField v-slot="{ controlAttrs }" label="人工核对依据（仅管理员可见）" name="admin-wallet-note"><UiInput v-model="note" v-bind="controlAttrs" maxlength="500" :disabled="saving || Boolean(reportIntent)" /></FormField>
        <UiButton type="submit" :loading="saving" :disabled="reportBlocked || Boolean(reportIntent?.result)">{{ reportIntent && !reportIntent.result ? '重试原结果记录' : '记录人工核对结果' }}</UiButton>
        <UiButton v-if="reportIntent?.result" type="button" variant="secondary" :disabled="saving" @click="newReport">添加新的核对记录</UiButton>
      </form>
      <form v-if="selected.external_status === 'failed' && !selected.reversal_entry_id" data-test="reverse" @submit.prevent="reverse">
        <FormField v-slot="{ controlAttrs }" label="恢复余额原因（用户可见）" name="admin-wallet-reversal"><UiInput v-model="reversalReason" v-bind="controlAttrs" maxlength="400" :disabled="saving" /></FormField>
        <UiButton type="submit" :loading="saving" :disabled="reversalReason.trim().length < 3">确认失败并恢复本笔扣减</UiButton>
      </form>
    </section>
  </UiSection>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useAppStore } from '../stores/app'
import { useRemoteResource } from '../composables/useRemoteResource'
import { newCheckoutIntent } from '../composables/checkoutOrder'
import { commerceErrorMessage } from '../utils/commerceErrors'
import { formatWalletMoney, parseWalletAmount, walletMajor } from '../utils/walletMoney'
import { debitWallet, fetchAdministrativeWallet, fetchWalletAdjustments, fetchWalletAdjustment, recordWalletPayout, reverseWalletAdjustment, type WalletAdjustment, type WalletDebit, type WalletPayout } from '../api/walletAdjustments'
import UiSection from './UiSection.vue'
import UiInput from './UiInput.vue'
import UiSelect from './UiSelect.vue'
import UiButton from './UiButton.vue'
import FormField from './FormField.vue'
import PageAlert from './PageAlert.vue'
import TimeBadge from './TimeBadge.vue'
const props = defineProps<{ userId: number }>(), app = useAppStore()
const actor = computed(() => app.user?.id || 0)
const currency = ref('CNY'), currencyInput = ref('CNY'), after = ref(0), ledgerAfter = ref(0)
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
const validDebit = computed(() => parseWalletAmount(amount.value) !== null && Number.isSafeInteger(Number(ticket.value)) && Number(ticket.value) > 0 && reason.value.trim().length >= 3)
function money(value: string) { return formatWalletMoney(value, currency.value) }
function label(value: string) { return ({ pending: '待核对外部出款', unknown: '外部结果未知', succeeded: '人工确认出款成功', failed: '人工确认出款失败' } as Record<string,string>)[value] || '结果未知' }
function applyCurrency() { if (saving.value) return; const next = currencyInput.value.trim().toUpperCase(); if (!/^[A-Z]{3,16}$/.test(next)) { writeError.value = '请输入有效币种。'; return }; if (currency.value === next) void load(); else currency.value = next }
async function act(work: () => Promise<void>) { if (saving.value || !actor.value) return; const original = scope(); saving.value = true; writeError.value = ''; try { await work() } catch (cause) { if (original === scope()) writeError.value = commerceErrorMessage(cause, '操作结果暂无法确认，请先核对记录，再明确重试原请求。') } finally { saving.value = false } }
async function openCase(id: number) { const original = scope(); await act(async () => { const value = await fetchWalletAdjustment(id); if (original !== scope()) return; selected.value = value; reportBlocked.value = true; reportIntent.value = restore<WalletPayout>(reportScope()); reportBlocked.value = false; outcome.value = reportIntent.value?.request.outcome || 'unknown'; reference.value = reportIntent.value?.request.reference || ''; note.value = reportIntent.value?.request.note || ''; reversalReason.value = '' }) }
async function debit() { if (debitBlocked.value) return; await act(async () => { if (!debitIntent.value && !validDebit.value) return; const key = scope(); const intent = debitIntent.value || { request: { user_id: props.userId, currency: currency.value, amount_minor: parseWalletAmount(amount.value)!, ticket_id: Number(ticket.value), reason: reason.value.trim(), idempotency_key: newCheckoutIntent() } }; if (intent.result) return; persist(key, intent); debitIntent.value = intent; const value = await debitWallet(intent.request); intent.result = value.id; persist(key, intent); if (key !== scope()) return; selected.value = value; reportIntent.value = null; await load() }) }
function newDebit() { if (!debitIntent.value?.result) return; localStorage.removeItem(scope()); debitIntent.value = null; amount.value = ''; ticket.value = ''; reason.value = '' }
async function report() { if (reportBlocked.value) return; await act(async () => { if (!selected.value) return; const original = scope(), id = selected.value.id, key = reportScope(); const intent = reportIntent.value || { request: { outcome: outcome.value, reference: reference.value.trim(), note: note.value.trim(), occurred_at: new Date().toISOString(), idempotency_key: newCheckoutIntent() } }; if (intent.result) return; persist(key, intent); reportIntent.value = intent; const value = await recordWalletPayout(id, intent.request); intent.result = value.id; persist(key, intent); if (original !== scope()) return; const detail = await fetchWalletAdjustment(id); if (original !== scope()) return; selected.value = detail; await load() }) }
function newReport() { if (!reportIntent.value?.result) return; localStorage.removeItem(reportScope()); reportIntent.value = null; outcome.value = 'unknown'; reference.value = ''; note.value = '' }
async function reverse() { await act(async () => { if (!selected.value || reversalReason.value.trim().length < 3) return; const original = scope(); const value = await reverseWalletAdjustment(selected.value.id, reversalReason.value.trim()); if (original !== scope()) return; selected.value = value; reversalReason.value = ''; await load() }) }
watch(() => [props.userId, actor.value, currency.value], () => { resource.reset(); after.value = 0; ledgerAfter.value = 0; selected.value = null; reportIntent.value = null; debitIntent.value = null; amount.value = ''; ticket.value = ''; reason.value = ''; outcome.value = 'unknown'; reference.value = ''; note.value = ''; reversalReason.value = ''; writeError.value = ''; debitBlocked.value = true; reportBlocked.value = false; if (props.userId && actor.value) { try { debitIntent.value = restore<WalletDebit>(scope()); if (debitIntent.value) { if (debitIntent.value.request.user_id !== props.userId || debitIntent.value.request.currency !== currency.value) throw new Error('扣减草稿与当前账户或币种不符，请先核对原记录。'); amount.value = walletMajor(debitIntent.value.request.amount_minor); ticket.value = String(debitIntent.value.request.ticket_id); reason.value = debitIntent.value.request.reason }; debitBlocked.value = false } catch (cause) { writeError.value = String(cause); return }; void load() } }, { immediate: true })
</script>
