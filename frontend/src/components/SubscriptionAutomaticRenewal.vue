<template>
  <UiSection :title="`订阅 #${subscriptionId} 的余额自动续费`" description="每份订阅独立开启，默认关闭；仅扣站内可用余额，不调用外部支付渠道。">
    <div class="panel-body stack">
      <PageAlert v-if="error" tone="danger" title="自动续费需要核对">{{ error }}</PageAlert>
      <PageAlert v-if="view" :tone="['insufficient_balance', 'history_review_required'].includes(view.state) ? 'warning' : 'info'" :title="view.state === 'history_review_required' ? '本项自动扣款已暂停' : view.enabled ? '已开启' : '已关闭'">
        {{ view.message }}
        <span v-if="view.last_order_id">最近订单 #{{ view.last_order_id }}。</span>
        <span v-if="view.enabled && view.next_attempt_at">下次检查 <TimeBadge :value="view.next_attempt_at" />。</span>
      </PageAlert>
      <p>到期后按已购周期锁价续费；余额不足会在此显示提醒，并在恢复窗口内继续检查。主动退订或窗口结束后停止；换购或续费条件变化时需要重新确认。</p>
      <FormField v-slot="{ controlAttrs }" label="续费规格" :name="`auto-renew-sku-${subscriptionId}`">
        <UiSelect v-model.number="sku" v-bind="controlAttrs" :options="skuOptions" :disabled="loading || saving" @change="loadOffer" />
      </FormField>
      <div class="inline-actions">
        <UiButton variant="secondary" size="sm" :disabled="loading || saving || page === 0" @click="loadSKUs(page - 1)">上一页规格</UiButton>
        <span>第 {{ page + 1 }} 页，共 {{ total }} 个续费规格</span>
        <UiButton variant="secondary" size="sm" :disabled="loading || saving || (page + 1) * pageSize >= total" @click="loadSKUs(page + 1)">下一页规格</UiButton>
      </div>
      <p v-if="offer">确认该规格每 {{ offer.billing_value }} {{ periodLabel(offer.billing_unit) }} 的基础续费价格为 {{ formatWalletMoney(offer.base_price_minor, offer.currency) }}。无法核验的优惠约定会暂停自动扣款。</p>
      <div class="inline-actions">
        <UiButton variant="secondary" :disabled="loading || saving" @click="reload">核对当前状态</UiButton>
        <UiButton v-if="offer" :disabled="loading || saving || !view || offer.plan_sku_id !== sku" :loading="saving" @click="save(true)">确认此规格并开启余额自动续费</UiButton>
        <UiButton v-if="view?.enabled" variant="secondary" :disabled="loading || saving" @click="save(false)">关闭余额自动续费</UiButton>
      </div>
    </div>
  </UiSection>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useAppStore } from '../stores/app'
import { fetchPlanCatalogSKUs, type PlanSKU } from '../api/client'
import { fetchAutomaticRenewal, setAutomaticRenewal, type AutomaticRenewalView, type AutomaticRenewalOffer } from '../api/automaticRenewal'
import { commerceErrorMessage } from '../utils/commerceErrors'
import { formatWalletMoney } from '../utils/walletMoney'
import UiSection from './UiSection.vue'
import UiButton from './UiButton.vue'
import UiSelect from './UiSelect.vue'
import FormField from './FormField.vue'
import PageAlert from './PageAlert.vue'
import TimeBadge from './TimeBadge.vue'

const props = defineProps<{ subscriptionId: number; planId: number }>()
const app = useAppStore(), actor = computed(() => app.user?.id || 0)
const view = ref<AutomaticRenewalView | null>(null), offer = ref<AutomaticRenewalOffer | null>(null)
const sku = ref(0), rows = ref<PlanSKU[]>([]), selected = ref<PlanSKU | null>(null)
const loading = ref(false), saving = ref(false), error = ref(''), page = ref(0), total = ref(0), pageSize = 25
let generation = 0, offerRequest = 0, pageRequest = 0
const skuOptions = computed(() => {
  const items = selected.value && !rows.value.some(row => row.id === selected.value?.id) ? [selected.value, ...rows.value] : rows.value
  return items.map(row => ({ value: row.id, label: `${row.name} · 每${row.billing_value}${periodLabel(row.billing_unit)}` }))
})
function periodLabel(unit: string) { return ({ day: '天', month: '月', year: '年' } as Record<string, string>)[unit] || unit }
function checked(next: AutomaticRenewalView) {
  if (next.subscription_id !== props.subscriptionId) throw new Error('自动续费结果所属订阅不一致，请重新核对。')
  return next
}
watch(() => [actor.value, props.subscriptionId, props.planId], () => {
  generation++; offerRequest++; pageRequest++
  view.value = null; offer.value = null; rows.value = []; selected.value = null; sku.value = 0; total.value = 0; page.value = 0; error.value = ''; loading.value = false; saving.value = false
  if (actor.value) void reload()
}, { immediate: true })
async function reload() {
  if (loading.value || saving.value || !actor.value) return
  const version = generation; loading.value = true; error.value = ''; offer.value = null
  try {
    const next = checked(await fetchAutomaticRenewal(props.subscriptionId))
    if (version !== generation) return
    view.value = next; offer.value = next.offer || null; sku.value = next.offer?.plan_sku_id || next.plan_sku_id
    await loadSKUs(0, version, true)
  } catch (cause) { if (version === generation) error.value = commerceErrorMessage(cause, '当前状态无法核对，请明确刷新后再操作。') }
  finally { if (version === generation) loading.value = false }
}
async function loadSKUs(nextPage: number, expected = generation, initial = false) {
  if (!actor.value || saving.value) return
  const request = ++pageRequest
  if (!initial) loading.value = true
  try {
    const result = await fetchPlanCatalogSKUs(props.planId, { operation: 'renew', offset: nextPage * pageSize, limit: pageSize, anchorId: initial && sku.value ? sku.value : undefined })
    if (expected !== generation || request !== pageRequest) return
    rows.value = result.items; total.value = result.total; page.value = Math.floor(result.offset / pageSize)
    selected.value = rows.value.find(row => row.id === sku.value) || selected.value
  } catch (cause) { if (expected === generation && request === pageRequest) error.value = commerceErrorMessage(cause, '续费规格加载失败，已知自动续费状态仍保留。') }
  finally { if (!initial && expected === generation && request === pageRequest) loading.value = false }
}
async function loadOffer() {
  if (!actor.value || saving.value || !sku.value) return
  const version = generation, request = ++offerRequest, requestedSKU = sku.value
  selected.value = rows.value.find(row => row.id === requestedSKU) || selected.value
  loading.value = true; error.value = ''; offer.value = null
  try {
    const next = checked(await fetchAutomaticRenewal(props.subscriptionId, requestedSKU))
    if (version !== generation || request !== offerRequest || sku.value !== requestedSKU) return
    view.value = next; offer.value = next.offer || null
  } catch (cause) { if (version === generation && request === offerRequest) error.value = commerceErrorMessage(cause, '该规格报价无法核对，尚未更改自动续费。') }
  finally { if (version === generation && request === offerRequest) loading.value = false }
}
async function save(enabled: boolean) {
  if (loading.value || saving.value || !view.value || !actor.value || (enabled && (!offer.value || offer.value.plan_sku_id !== sku.value))) return
  const version = generation, id = props.subscriptionId
  const request = { enabled, plan_sku_id: enabled ? sku.value : view.value.plan_sku_id, quote_fingerprint: enabled ? offer.value!.quote_fingerprint : '', expected_revision: view.value.revision }
  saving.value = true; error.value = ''
  try {
    const next = checked(await setAutomaticRenewal(id, request))
    if (version !== generation) return
    view.value = next; offer.value = null
  } catch (cause) {
    if (version !== generation) return
    offer.value = null
    error.value = commerceErrorMessage(cause, '操作结果暂无法确认，请点击核对当前状态；不会自动重放开启或关闭请求。')
  } finally { if (version === generation) saving.value = false }
}
</script>
