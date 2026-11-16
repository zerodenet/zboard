<template>
  <form ref="formElement" class="stack" aria-label="编辑订阅设置" novalidate @submit.prevent="save">
    <PageAlert v-if="formErrors.formError.value" tone="danger" title="订阅设置保存失败">{{ formErrors.formError.value }}</PageAlert>
    <TransientFeedback :success="success ? '订阅设置已保存。' : ''" />
    <div class="form-grid form-grid-2">
    <FormField v-if="editSchedule" v-slot="{ controlAttrs }" label="到期时间" name="subscription-end-at" :error="fields.end_at" :hint="`时区：${editorTimeZone}${subscription.end_at?.startsWith('9999-') ? '；留空保留长期有效。' : ''}`">
      <UiInput v-model="endAt" v-bind="controlAttrs" type="datetime-local" :disabled="saving" />
    </FormField>
    <FormField v-if="editReset && periodic" v-slot="{ controlAttrs }" label="下次重置" name="subscription-next-reset-at" :error="fields.next_reset_at" :hint="`时区：${editorTimeZone}；仅设置下一次，之后按重置规则执行。`">
      <UiInput v-model="nextResetAt" v-bind="controlAttrs" type="datetime-local" :disabled="saving" />
      <UiButton type="button" variant="ghost" size="sm" :disabled="saving || recalculate" @click="recalculate = true; nextResetAt = ''">重新计算</UiButton>
      <small v-if="recalculate || !nextResetAt">保存时按重置规则计算。</small>
    </FormField>
    <FormField v-if="editQuota" v-slot="{ controlAttrs }" label="当前周期配额" name="subscription-quota" :error="fields.flow_total">
      <ByteSizeInput v-model="total" v-bind="controlAttrs" :min-bytes="0" :max-bytes="Number.MAX_SAFE_INTEGER" :disabled="saving" />
    </FormField>
    <FormField v-if="editQuota" v-slot="{ controlAttrs }" label="当前周期已用" name="subscription-used" :error="fields.flow_used">
      <ByteSizeInput v-model="used" v-bind="controlAttrs" :min-bytes="0" :max-bytes="Number.MAX_SAFE_INTEGER" :disabled="saving" />
    </FormField>
    <FormField v-if="editReset" v-slot="{ controlAttrs }" label="每次重置配额" name="subscription-reset-quota" :error="fields.reset_quota_bytes">
      <ByteSizeInput v-model="resetQuota" v-bind="controlAttrs" :min-bytes="0" :max-bytes="Number.MAX_SAFE_INTEGER" :disabled="saving" />
    </FormField>
    <FormField v-if="editReset" v-slot="{ controlAttrs }" label="流量重置" name="subscription-reset-policy" :error="fields.reset_policy">
      <UiSelect v-model.number="resetPolicy" v-bind="controlAttrs" :options="policyOptions" :disabled="saving" />
    </FormField>

    <small v-if="editReset && (scheduleChanged || changes.next_reset_at)" class="field-hint field-full">当前用量不清零。</small>
    <FormField v-slot="{ controlAttrs }" label="调整原因" name="subscription-quota-reason" :error="fields.reason" required full>
      <UiTextarea v-model="reason" v-bind="controlAttrs" :disabled="saving" maxlength="255" rows="2" />
    </FormField>
    </div>
    <div class="quota-editor-footer"><span v-if="editQuota">剩余 {{ formatBytes(Math.max(0, total - used)) }}</span><div><UiButton v-if="cancellable" type="button" variant="secondary" :disabled="saving" @click="cancel">取消</UiButton><UiButton type="submit" :disabled="!canSave" :loading="saving">保存设置</UiButton></div></div>
  </form>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { updateSubscriptionQuota, type AdminSubscriptionDetail } from '../api/client'
import ByteSizeInput from './ByteSizeInput.vue'
import FormField from './FormField.vue'
import PageAlert from './PageAlert.vue'
import TransientFeedback from './TransientFeedback.vue'
import UiButton from './UiButton.vue'
import UiTextarea from './UiTextarea.vue'
import UiSelect from './UiSelect.vue'
import UiInput from './UiInput.vue'
import { dateTimeInputValue, dateTimeInputISO } from '../utils/dateTimeInput'
import { getDisplayTimeZone } from '../utils/timeZone'
import type { SubscriptionQuotaUpdate } from '../api/client'
import { formatBytes } from '../utils/format'
import { resetPolicyOptions } from '../utils/resetPolicy'
import { useFormErrors } from '../composables/useFormState'
import { utf8ByteLength } from '../utils/validation'
import { confirmAction } from '../utils/feedback'

const props = withDefaults(defineProps<{ subscription: AdminSubscriptionDetail; mode?: 'quota' | 'reset' | 'all'; cancellable?: boolean }>(), { mode: 'all', cancellable: false })
const emit = defineEmits<{ saved: []; cancel: [] }>()
const editQuota = computed(() => props.mode !== 'reset')
const editReset = computed(() => props.mode !== 'quota')
const editSchedule = computed(() => props.mode === 'all')
const editorTimeZone = ref(getDisplayTimeZone()), endAt = ref(''), nextResetAt = ref('')
const periodic = computed(() => resetPolicy.value >= 1 && resetPolicy.value <= 4)
const total = ref(0), used = ref(0), resetQuota = ref(0), reason = ref('')
const saving = ref(false), success = ref(false)
const resetPolicy = ref(0), recalculate = ref(false)
const scheduleChanged = computed(() => recalculate.value || resetPolicy.value !== (props.subscription.reset_policy || 0))
const policyOptions = computed(() => resetPolicyOptions.map(option => ({ ...option, disabled: option.value >= 1 && option.value <= 4 && (props.subscription.lifecycle === 'fixed' || props.subscription.end_at?.startsWith('9999-')) })))
const formElement = ref<HTMLElement | null>(null)
const formErrors = useFormErrors()
const { fields, clear, applyValidation, applyApiError } = formErrors
function expiryInput(value?: string) { return value?.startsWith('9999-') ? '' : dateTimeInputValue(value, editorTimeZone.value) }
let syncingSubscription = false
let attempt: { signature: string; id: string } | null = null
watch(() => props.subscription, sub => {
  syncingSubscription = true
  total.value = sub.flow_total; used.value = sub.flow_used; resetQuota.value = sub.reset_quota_bytes || 0
  resetPolicy.value = sub.reset_policy || 0; recalculate.value = false
  editorTimeZone.value = getDisplayTimeZone(); endAt.value = expiryInput(sub.end_at); nextResetAt.value = dateTimeInputValue(sub.next_reset_at, editorTimeZone.value)
  reason.value = ''; clear(); attempt = null; syncingSubscription = false
}, { immediate: true })
watch(total, () => clear('flow_total'))
watch(used, () => clear('flow_used'))
watch(resetQuota, () => clear('reset_quota_bytes'))
watch(resetPolicy, (value, prior) => { clear('reset_policy'); if (!syncingSubscription && value !== prior && prior !== undefined) { nextResetAt.value = ''; recalculate.value = true } }, { flush: 'sync' })
watch(endAt, () => clear('end_at'))
watch(nextResetAt, () => { clear('next_reset_at'); if (nextResetAt.value) recalculate.value = false })
watch(reason, () => clear('reason'))
const changes = computed(() => {
  const sub = props.subscription
  return {
    ...(editSchedule.value && endAt.value !== expiryInput(sub.end_at) ? { end_at: dateTimeInputISO(endAt.value, editorTimeZone.value) || endAt.value, expected_end_at: sub.end_at } : {}),
    ...(editReset.value && periodic.value && nextResetAt.value && (nextResetAt.value !== dateTimeInputValue(sub.next_reset_at, editorTimeZone.value) || scheduleChanged.value) ? { next_reset_at: dateTimeInputISO(nextResetAt.value, editorTimeZone.value) || nextResetAt.value, expected_next_reset_at: sub.next_reset_at || '' } : {}),
    ...(editReset.value && periodic.value && !nextResetAt.value && !scheduleChanged.value && Boolean(sub.next_reset_at) ? { reset_policy: resetPolicy.value } : {}),
    ...(editQuota.value && total.value !== sub.flow_total ? { flow_total: total.value } : {}),
    ...(editQuota.value && used.value !== sub.flow_used ? { flow_used: used.value } : {}),
    ...(editReset.value && resetQuota.value !== (sub.reset_quota_bytes || 0) ? { reset_quota_bytes: resetQuota.value } : {}),
    ...(editReset.value && scheduleChanged.value ? { reset_policy: resetPolicy.value } : {}),
  }
})
const hasChanges = computed(() => Object.keys(changes.value).length > 0)
function validationErrors() {
  const errors: Record<string, string> = {}
  for (const name of ['flow_total', 'flow_used', 'reset_quota_bytes'] as const) { const value = changes.value[name]; if (value !== undefined && (!Number.isSafeInteger(value) || value < 0)) errors[name] = '请填写不小于零的有效流量。' }
  const expiry = changes.value.end_at
  if (expiry !== undefined && (!dateTimeInputISO(endAt.value, editorTimeZone.value) || Date.parse(expiry) <= Date.parse(props.subscription.start_at))) errors.end_at = '请选择晚于订阅开始时间的有效日期。'
  const reset = changes.value.next_reset_at
  if (reset && (!dateTimeInputISO(nextResetAt.value, editorTimeZone.value) || Date.parse(reset) <= Date.now() || Date.parse(reset) < Date.parse(props.subscription.start_at) || Date.parse(reset) > Date.parse(expiry || props.subscription.end_at))) errors.next_reset_at = '下次重置须在未来，且不晚于到期时间。'
  if (editReset.value && !policyOptions.value.some(option => option.value === resetPolicy.value && !option.disabled)) errors.reset_policy = '请选择适用于此订阅的重置规则。'
  const length = utf8ByteLength(reason.value.trim())
  if (length < 3 || length > 255) errors.reason = '请填写调整原因，长度为 3 到 255 字节。'
  return errors
}
const canSave = computed(() => hasChanges.value && !saving.value && Object.keys(validationErrors()).length === 0)

async function confirmLeave() {
  if (saving.value) return false
  if (!hasChanges.value && !reason.value.trim()) return true
  return confirmAction({ title: '放弃未保存的修改？', message: '当前订阅设置尚未保存。', confirmText: '放弃修改', tone: 'danger' })
}
async function cancel() { if (await confirmLeave()) emit('cancel') }
defineExpose({ confirmLeave, dirty: computed(() => hasChanges.value || Boolean(reason.value.trim())), saving })
async function save() {
  if (saving.value || !hasChanges.value) return
  if (!await applyValidation(validationErrors(), formElement)) return
  const sub = props.subscription
  const payload: Omit<SubscriptionQuotaUpdate, 'idempotency_key'> = { ...changes.value, reason: reason.value.trim() }
  const signature = JSON.stringify([sub.id, payload])
  if (attempt?.signature !== signature) attempt = { signature, id: crypto.randomUUID() }
  saving.value = true; clear(); success.value = false
  try {
    await updateSubscriptionQuota(sub.id, { ...payload, idempotency_key: attempt.id })
    success.value = true; emit('saved')
  } catch (cause: any) {
    await applyApiError(cause, '保存失败，请重试。', formElement)
  } finally { saving.value = false }
}
</script>
<style scoped>
.quota-editor-footer { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
.quota-editor-footer > div { display: flex; gap: 8px; margin-left: auto; }
.quota-editor-footer > span { color: var(--muted); font-size: 12px; }
</style>
