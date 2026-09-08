<template>
  <ModalDialog :open="open" title="分配订单" description="为指定用户选择商品和规格，创建订单并保留分配记录。" :busy="saving" @close="emit('close')">
    <form id="assign-order-form" ref="formElement" class="modal-form" novalidate @submit.prevent="submit">
      <PageAlert v-if="formErrors.formError.value" tone="danger" title="订单分配失败">{{ formErrors.formError.value }}</PageAlert>
      <FormField v-slot="{ controlId }" label="用户" name="assign-user" required full :error="fields.user_id">
        <AdminOrderLookup v-model="user" :input-id="controlId" label="用户" placeholder="输入邮箱搜索用户" :fetch-page="loadUsers" :disabled="saving || userLoading" />
        <small v-if="userLoading">正在加载用户…</small>
      </FormField>
      <FormField v-slot="{ controlAttrs }" label="权益操作" name="assign-operation" full>
        <UiSelect v-model="operation" v-bind="controlAttrs" :options="operationOptions" :disabled="saving" />
      </FormField>
      <FormField v-if="operation !== 'purchase'" v-slot="{ controlId }" label="目标订阅" name="assign-target" required full :error="fields.target_subscription_id">
        <AdminOrderLookup :key="`target-${user?.id || 0}-${operation}`" v-model="target" :input-id="controlId" label="目标订阅" placeholder="搜索该用户的订阅" :fetch-page="loadTargets" :disabled="saving || !user" />
      </FormField>
      <FormField v-slot="{ controlId }" label="商品" name="assign-plan" required full :error="fields.plan_id">
        <AdminOrderLookup :key="`plan-${operation}-${target?.id || 0}`" v-model="plan" :input-id="controlId" label="商品" placeholder="输入商品名称搜索" :fetch-page="loadPlans" :disabled="saving || (operation !== 'purchase' && !target)" />
      </FormField>
      <FormField v-slot="{ controlId }" label="销售规格" name="assign-sku" required full :error="fields.plan_sku_id">
        <AdminOrderLookup :key="`sku-${plan?.id || 0}-${operation}`" v-model="sku" :input-id="controlId" label="销售规格" placeholder="选择销售规格" :fetch-page="loadSKUs" :disabled="saving || !plan" />
      </FormField>
      <FormField v-slot="{ controlAttrs }" label="应付金额" name="assign-amount" required full :error="fields.payable_amount" :hint="sku?.sku ? `规格原价 ${formatCurrency(sku.sku.price_cents, sku.sku.currency)}；可调整本笔订单的付款金额。` : '请先选择销售规格。'">
        <MoneyInput v-model="payableAmount" v-bind="controlAttrs" :currency="sku?.sku?.currency || 'CNY'" :min-cents="0" :max-cents="Number.MAX_SAFE_INTEGER" :step-cents="1" :disabled="saving || !sku" />
      </FormField>
      <PageAlert tone="info" title="等待用户付款">订单会出现在该用户的订单列表，付款确认后开通权益。<span v-if="sku?.sku">应付 {{ formatCurrency(payableAmount, sku.sku.currency) }}。</span></PageAlert>
      <FormField v-slot="{ controlAttrs }" label="分配原因" name="assign-note" required full :error="fields.note" hint="记录在管理员订单详情与审计日志中。">
        <UiTextarea v-model="note" v-bind="controlAttrs" :disabled="saving" :maxlength="500" rows="3" placeholder="例如：按客户要求代下单，选择月付规格" />
      </FormField>
    </form>
    <template #footer><UiButton variant="secondary" :disabled="saving" @click="emit('close')">取消</UiButton><UiButton type="submit" form="assign-order-form" :disabled="!canSubmit" :loading="saving">创建待付款订单</UiButton></template>
  </ModalDialog>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { assignAdminOrder, fetchAdminUserDetail, fetchUsersPage, fetchSubscriptionsPage, fetchPlanCatalogPage, fetchPlanCatalogSKUs, type AdminOrderDetail, type CatalogOperation } from '../api/client'
import ModalDialog from './ModalDialog.vue'
import FormField from './FormField.vue'
import PageAlert from './PageAlert.vue'
import UiSelect from './UiSelect.vue'
import UiTextarea from './UiTextarea.vue'
import MoneyInput from './MoneyInput.vue'
import UiButton from './UiButton.vue'
import AdminOrderLookup from './AdminOrderLookup.vue'
import type { AssignmentChoice } from '../utils/adminOrderAssignment'
import { commerceErrorMessage } from '../utils/commerceErrors'
import { useFormErrors } from '../composables/useFormState'
import { formatCurrency } from '../utils/format'
const props = defineProps<{ open: boolean; userId?: number }>()
const emit = defineEmits<{ close: []; assigned: [order: AdminOrderDetail] }>()
const user = ref<AssignmentChoice | null>(null), target = ref<AssignmentChoice | null>(null), plan = ref<AssignmentChoice | null>(null), sku = ref<AssignmentChoice | null>(null)
const operation = ref<CatalogOperation>('purchase'), note = ref('')
const payableAmount = ref(0)
const saving = ref(false), userLoading = ref(false)
const formElement = ref<HTMLElement | null>(null)
const formErrors = useFormErrors()
const { fields, formError, clear: clearErrors, applyValidation, applyApiError } = formErrors
const operationOptions = [{ label: '新开订阅', value: 'purchase' }, { label: '续费 / 补充额度', value: 'renew' }, { label: '切换套餐', value: 'change' }, { label: '添加流量包', value: 'addon' }]
const canSubmit = computed(() => !saving.value && !userLoading.value && !!user.value && !!plan.value && !!sku.value && !!note.value.trim() && Number.isSafeInteger(payableAmount.value) && payableAmount.value >= 0 && (operation.value === 'purchase' || !!target.value))
let hydration: AbortController | null = null
let attempt: { signature: string; id: string } | null = null
watch(user, () => { target.value = null; clearErrors('user_id') })
watch(operation, () => { target.value = null; plan.value = null })
watch(target, () => { plan.value = null; clearErrors('target_subscription_id') })
watch(plan, () => { sku.value = null; clearErrors('plan_id') })
watch(sku, value => { payableAmount.value = value?.sku?.price_cents || 0; clearErrors('plan_sku_id') })
watch(payableAmount, () => clearErrors('payable_amount'))
watch(note, () => clearErrors('note'))
watch(() => [props.open, props.userId] as const, async ([open, id]) => {
  hydration?.abort(); hydration = new AbortController(); const current = hydration
  if (!open) return
  user.value = null; target.value = null; plan.value = null; sku.value = null; operation.value = 'purchase'; note.value = ''; clearErrors(); attempt = null
  userLoading.value = !!id
  if (!id) return
  try { const detail = await fetchAdminUserDetail(id, { signal: current.signal }); if (!current.signal.aborted) user.value = { id: detail.id, label: `${detail.email} · #${detail.id}` } }
  catch { if (!current.signal.aborted) formError.value = '指定用户加载失败，请重新搜索并选择用户。' }
  finally { if (!current.signal.aborted) userLoading.value = false }
}, { immediate: true })
async function loadUsers(q: string, offset: number, signal: AbortSignal) {
  const page = await fetchUsersPage({ q, offset, limit: 25, status: 'active' }, { signal })
  return { ...page, items: page.items.map(item => ({ id: item.id, label: `${item.email} · #${item.id}` })) }
}
async function loadTargets(q: string, offset: number, signal: AbortSignal) {
  if (!user.value) return { items: [], total: 0 }
  const page = await fetchSubscriptionsPage({ q, offset, limit: 25, userId: user.value.id }, { signal })
  return { ...page, items: page.items.map(item => ({ id: item.id, planId: item.plan_id, label: `#${item.id} · ${item.plan_name} / ${item.sku_name}` })) }
}
async function loadPlans(q: string, offset: number, signal: AbortSignal) {
  const page = await fetchPlanCatalogPage({ q, offset, limit: 25, operation: operation.value, planId: operation.value === 'renew' || operation.value === 'addon' ? target.value?.planId : undefined, excludePlanId: operation.value === 'change' ? target.value?.planId : undefined }, { signal })
  return { ...page, items: page.items.map(item => ({ id: item.id, label: `${item.name} · #${item.id}` })) }
}
async function loadSKUs(q: string, offset: number, signal: AbortSignal) {
  if (!plan.value) return { items: [], total: 0 }
  const page = await fetchPlanCatalogSKUs(plan.value.id, { q, offset, limit: 25, operation: operation.value }, { signal })
  return { ...page, items: page.items.map(item => ({ id: item.id, label: `${item.name} · ${formatCurrency(item.price_cents, item.currency)} · #${item.id}`, sku: item })) }
}
async function submit() {
  if (saving.value || userLoading.value) return
  const validation: Record<string, string> = {}
  if (!user.value) validation.user_id = '请选择用户。'
  if (!plan.value) validation.plan_id = '请选择商品。'
  if (!sku.value) validation.plan_sku_id = '请选择销售规格。'
  if (operation.value !== 'purchase' && !target.value) validation.target_subscription_id = '请选择目标订阅。'
  if (!note.value.trim()) validation.note = '请填写分配原因。'
  if (!Number.isSafeInteger(payableAmount.value) || payableAmount.value < 0) validation.payable_amount = '应付金额必须为不小于 0 的整数分。'
  if (!await applyValidation(validation, formElement)) return
  if (saving.value) return
  const payload = { user_id: user.value!.id, plan_sku_id: sku.value!.id, target_subscription_id: operation.value === 'purchase' ? undefined : target.value!.id, note: note.value.trim(), payable_amount: payableAmount.value }
  const signature = JSON.stringify(payload)
  if (!attempt || attempt.signature !== signature) attempt = { signature, id: crypto.randomUUID() }
  saving.value = true; clearErrors()
  try { const order = await assignAdminOrder({ ...payload, request_id: attempt.id }); emit('assigned', order) }
  catch (cause: any) { await applyApiError(cause, commerceErrorMessage(cause, '订单分配失败，请重试。'), formElement) }
  finally { saving.value = false }
}
onBeforeUnmount(() => hydration?.abort())
</script>
