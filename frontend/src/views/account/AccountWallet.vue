<template>
  <section class="account-page stack">
    <PageHeader title="账户余额">
      <template #actions><PageRefreshButton label="刷新余额" :loading="loading" @click="load" /></template>
    </PageHeader>
    <div class="wallet-overview">
      <UiSection title="当前余额">
        <template #actions><UiInput id="wallet-currency" v-model="currency" class="wallet-currency" aria-label="币种" maxlength="3" :disabled="submitted" /></template>
        <div class="account-section-body wallet-balances">
          <BalanceSummary v-if="wallet" :balance="wallet" :show-total="false" />
          <p v-else-if="loading" role="status">正在核对余额…</p>
          <PageAlert v-if="error" tone="danger">{{ error }}<template #actions><UiButton variant="secondary" size="sm" :loading="loading" @click="load">重试</UiButton></template></PageAlert>
        </div>
      </UiSection>
      <UiSection title="充值">
        <template #actions><UiButton as-child variant="ghost" size="sm"><RouterLink to="/account/orders">充值记录</RouterLink></UiButton></template>
        <div class="account-section-body">
          <form class="account-form" @submit.prevent="create" novalidate>
            <FormField v-slot="{ controlAttrs }" :label="`充值金额（${currency.trim().toUpperCase()}）`" name="wallet-topup-amount"><UiInput v-model="amount" v-bind="controlAttrs" inputmode="decimal" placeholder="例如 10.00" :disabled="submitted" /></FormField>
            <PageAlert v-if="createError" tone="danger">{{ createError }}</PageAlert>
            <div class="account-form-actions"><UiButton type="submit" :loading="creating">{{ submitted ? '继续充值' : '充值' }}</UiButton><UiButton v-if="submitted" type="button" variant="secondary" :disabled="creating" @click="startNew">新建另一笔充值</UiButton></div>
          </form>
        </div>
      </UiSection>
    </div>
    <UiSection title="余额流水">
      <DataTable v-if="wallet?.entries.length" caption="余额流水" :min-width="650">
        <thead><tr><th data-column-priority="2">时间</th><th class="table-primary-column">业务</th><th class="numeric-column">金额变动（{{ wallet.currency }}）</th><th class="numeric-column" data-column-priority="2">变动后余额</th><th data-column-priority="3">来源</th><th class="table-action-column"><span class="sr-only">操作</span></th></tr></thead>
        <tbody><tr v-for="entry in wallet.entries" :key="entry.id"><td data-column-priority="2"><TimeBadge :value="entry.created_at" /></td><td class="table-primary-column"><TableText :value="entry.reason" /><span class="wallet-entry-time"><TimeBadge :value="entry.created_at" /></span></td><td class="numeric-column" :aria-label="formatWalletMoney(entry.balance_delta, wallet.currency)">{{ money(entry.balance_delta) }}</td><td class="numeric-column" data-column-priority="2"><BalanceValue :amount="entry.balance_after" :currency="wallet.currency" :show-currency="false" /></td><td data-column-priority="3"><RouterLink v-if="entry.source_order_id" :to="`/account/orders/${entry.source_order_id}`">订单 #{{ entry.source_order_id }}</RouterLink><span v-else>-</span></td><td class="table-action-column"><UiButton v-if="Number.isSafeInteger(entry.id) && entry.id > 0" as-child variant="ghost" icon size="sm"><RouterLink aria-label="就此流水提交工单" title="就此流水提交工单" :to="{ path: '/account/tickets', query: { new: '1', wallet_entry_id: String(entry.id) } }"><UiIcon name="ticket" /></RouterLink></UiButton></td></tr></tbody>
      </DataTable>
      <EmptyState v-else-if="wallet" icon="billing" title="暂无余额流水" description="" />
      <div v-if="wallet?.has_more || after" class="account-section-footer"><UiButton v-if="wallet?.has_more" variant="secondary" :disabled="loading" @click="older">较早流水</UiButton><UiButton v-if="after" variant="secondary" :disabled="loading" @click="after = 0; load()">最新流水</UiButton></div>
    </UiSection>
  </section>
</template>
<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAppStore } from '../../stores/app'
import { fetchWallet, createWalletTopUp, type WalletPage } from '../../api/wallet'
import { ensureCheckoutOrder, newCheckoutIntent, restoreCheckoutIntent } from '../../composables/checkoutOrder'
import { useRemoteResource } from '../../composables/useRemoteResource'
import UiIcon from '../../components/UiIcon.vue'
import TableText from '../../components/TableText.vue'
import EmptyState from '../../components/EmptyState.vue'
import BalanceSummary from '../../components/BalanceSummary.vue'
import BalanceValue from '../../components/BalanceValue.vue'
import { parseWalletAmount, formatWalletMoney, walletMajor } from '../../utils/walletMoney'
import { commerceErrorMessage } from '../../utils/commerceErrors'
import PageRefreshButton from '../../components/PageRefreshButton.vue'
import PageHeader from '../../components/PageHeader.vue'
import UiSection from '../../components/UiSection.vue'
import UiButton from '../../components/UiButton.vue'
import UiInput from '../../components/UiInput.vue'
import FormField from '../../components/FormField.vue'
import PageAlert from '../../components/PageAlert.vue'
import DataTable from '../../components/DataTable.vue'
import TimeBadge from '../../components/TimeBadge.vue'
const app = useAppStore(), router = useRouter(), route = useRoute()
const currency = ref(String(route.query.currency || 'CNY').toUpperCase()), amount = ref(''), after = ref(0)
const submitted = ref(false), creating = ref(false), createError = ref(''), intent = ref('')
const resource = useRemoteResource<WalletPage | null>({ initial: () => null, fetch: ({ signal }) => fetchWallet(currency.value.trim().toUpperCase(), after.value, signal), errorMessage: '余额暂时无法核对，请重试。' })
const { data: wallet, loading, error, load } = resource
function activeKey() { return `zboard-wallet-active:${app.user.id}` }
function draftKey() { return `zboard-wallet-topup:${app.user.id}:${intent.value}` }
function money(value: string) { return /^-?\d+$/.test(value) ? walletMajor(value) : '金额未知' }
function restore() {
  intent.value = restoreCheckoutIntent(localStorage, activeKey())
  try {
    const draft = JSON.parse(localStorage.getItem(draftKey()) || 'null')
    if (draft?.request && Number.isSafeInteger(draft.request.amount_minor) && /^[A-Z]{3}$/.test(draft.request.currency)) {
      amount.value = walletMajor(draft.request.amount_minor); currency.value = draft.request.currency; submitted.value = true
    }
  } catch { createError.value = '充值创建记录无法读取，请先在我的订单核对。' }
}
function startNew() {
  intent.value = newCheckoutIntent(); localStorage.setItem(activeKey(), intent.value)
  amount.value = ''; submitted.value = false; createError.value = ''
}
async function create() {
  if (creating.value || !app.user.id) return
  const minor = parseWalletAmount(amount.value), selected = currency.value.trim().toUpperCase()
  if (minor == null || !/^[A-Z]{3}$/.test(selected)) { createError.value = '请输入正确币种及正金额，最多两位小数。'; return }
  creating.value = true; submitted.value = true; createError.value = ''
  const actor = app.user.id
  try {
    const id = await ensureCheckoutOrder(localStorage, draftKey(), { amount_minor: minor, currency: selected }, (request, key) => createWalletTopUp({ ...request, idempotency_key: key }))
    if (actor === app.user.id) await router.push(`/account/orders/${id}`)
  } catch (cause) { if (actor === app.user.id) createError.value = commerceErrorMessage(cause, '充值单结果暂无法确认，请使用本次标识恢复，或在我的订单核对。') }
  finally { creating.value = false }
}
function older() { after.value = wallet.value?.entries.at(-1)?.id || 0; void load() }
watch(() => app.user.id, () => { resource.reset(); amount.value = ''; submitted.value = false; createError.value = ''; after.value = 0; if (app.user.id) { restore(); void load() } }, { immediate: true })
watch(currency, () => { after.value = 0; resource.reset(); if (app.user.id) void load() })
</script>

<style scoped>
.wallet-entry-time { display: none; }
@media (max-width: 600px) { .wallet-entry-time { display: block; margin-top: 4px; } }
.wallet-overview { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 24px; align-items: stretch; }
.wallet-currency { width: 96px; }
.wallet-balances { min-height: 150px; align-content: center; }
.wallet-balances :deep(.balance-summary dd) { font-size: 28px; font-weight: 600; }
@media (max-width: 800px) { .wallet-overview { grid-template-columns: minmax(0, 1fr); gap: 20px; } .wallet-balances { min-height: auto; } }
</style>
