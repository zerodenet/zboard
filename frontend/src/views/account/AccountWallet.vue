<template>
  <section class="account-page stack">
    <PageHeader title="账户余额" eyebrow="WALLET" description="余额与预留按币种分别记账，不提供提现。">
      <template #actions><UiButton variant="secondary" :disabled="loading" @click="load">刷新余额</UiButton></template>
    </PageHeader>
    <UiSection title="当前余额">
      <FormField v-slot="{ controlAttrs }" label="币种" name="wallet-currency"><UiInput v-model="currency" v-bind="controlAttrs" maxlength="3" :disabled="submitted" /></FormField>
      <p v-if="wallet">可用 {{ money(wallet.available_minor) }} · 已预留 {{ money(wallet.reserved_minor) }}</p>
      <p v-else-if="loading" role="status">正在核对余额…</p>
      <PageAlert v-if="error" tone="danger">{{ error }}</PageAlert>
    </UiSection>
    <UiSection title="主动充值" description="创建充值单后选择已接入的支付渠道。只有渠道付款核验完成，资金才会进入余额。">
      <form @submit.prevent="create" novalidate>
        <FormField v-slot="{ controlAttrs }" label="充值金额" name="wallet-topup-amount"><UiInput v-model="amount" v-bind="controlAttrs" inputmode="decimal" placeholder="例如 10.00" :disabled="submitted" /></FormField>
        <PageAlert v-if="createError" tone="danger">{{ createError }}</PageAlert>
        <UiButton type="submit" :loading="creating">{{ submitted ? '恢复本次充值单' : '创建充值单并选择支付方式' }}</UiButton>
        <UiButton v-if="submitted" type="button" variant="secondary" :disabled="creating" @click="startNew">新建另一笔充值</UiButton>
      </form>
      <RouterLink to="/account/orders">核对我的订单与已有充值</RouterLink>
    </UiSection>
    <UiSection title="余额流水">
      <DataTable v-if="wallet?.entries.length" caption="余额流水" :min-width="650">
        <thead><tr><th>时间</th><th>业务</th><th>金额变动</th><th>变动后余额</th><th>来源</th></tr></thead>
        <tbody><tr v-for="entry in wallet.entries" :key="entry.id"><td><TimeBadge :value="entry.created_at" /></td><td>{{ entry.reason }}</td><td>{{ money(entry.balance_delta) }}</td><td>{{ money(entry.balance_after) }}</td><td><RouterLink v-if="entry.source_order_id" :to="`/account/orders/${entry.source_order_id}`">订单 #{{ entry.source_order_id }}</RouterLink></td></tr></tbody>
      </DataTable>
      <p v-else-if="wallet">暂无该币种余额流水。</p>
      <UiButton v-if="wallet?.has_more" variant="secondary" :disabled="loading" @click="older">较早流水</UiButton>
      <UiButton v-if="after" variant="secondary" :disabled="loading" @click="after = 0; load()">最新流水</UiButton>
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
import { parseWalletAmount, formatWalletMoney, walletMajor } from '../../utils/walletMoney'
import { commerceErrorMessage } from '../../utils/commerceErrors'
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
function money(value: string) { return formatWalletMoney(value, wallet.value?.currency || currency.value) }
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
