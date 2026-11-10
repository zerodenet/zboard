<template>
  <form ref="formElement" class="stack" aria-label="编辑订阅流量与重置" novalidate @submit.prevent="save">
    <PageAlert v-if="formErrors.formError.value" tone="danger" title="订阅设置保存失败">{{ formErrors.formError.value }}</PageAlert>
    <TransientFeedback :success="success ? '流量与重置设置已保存。' : ''" />
    <div class="form-grid form-grid-2">
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
    <div v-if="editReset" class="reset-schedule">
      <span>下次重置</span>
      <div><span>{{ scheduleChanged ? resetPolicy >= 1 && resetPolicy <= 4 ? '保存时重新计算' : '-' : formatDateTime(subscription.next_reset_at) }}</span><UiButton v-if="resetPolicy >= 1 && resetPolicy <= 4 && !scheduleChanged" type="button" variant="ghost" size="sm" :disabled="saving" @click="recalculate = true">重新计算</UiButton></div>
      <small v-if="scheduleChanged">仅调整后续安排，当前用量不清零。</small>
    </div>
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
import { formatBytes, formatDateTime } from '../utils/format'
import { resetPolicyOptions } from '../utils/resetPolicy'
import { useFormErrors } from '../composables/useFormState'
import { utf8ByteLength } from '../utils/validation'
import { confirmAction } from '../utils/feedback'

const props = withDefaults(defineProps<{ subscription: AdminSubscriptionDetail; mode?: 'quota' | 'reset' | 'all'; cancellable?: boolean }>(), { mode: 'all', cancellable: false })
const emit = defineEmits<{ saved: []; cancel: [] }>()
const editQuota = computed(() => props.mode !== 'reset')
const editReset = computed(() => props.mode !== 'quota')
const total = ref(0), used = ref(0), resetQuota = ref(0), reason = ref('')
const saving = ref(false), success = ref(false)
const resetPolicy = ref(0), recalculate = ref(false)
const scheduleChanged = computed(() => recalculate.value || resetPolicy.value !== (props.subscription.reset_policy || 0))
const policyOptions = computed(() => resetPolicyOptions.map(option => ({ ...option, disabled: option.value >= 1 && option.value <= 4 && (props.subscription.lifecycle === 'fixed' || props.subscription.end_at?.startsWith('9999-')) })))
const formElement = ref<HTMLElement | null>(null)
const formErrors = useFormErrors()
const { fields, clear, applyValidation, applyApiError } = formErrors
let attempt: { signature: string; id: string } | null = null
watch(() => props.subscription, sub => {
  total.value = sub.flow_total; used.value = sub.flow_used; resetQuota.value = sub.reset_quota_bytes || 0
  resetPolicy.value = sub.reset_policy || 0; recalculate.value = false
  reason.value = ''; clear(); attempt = null
}, { immediate: true })
watch(total, () => clear('flow_total'))
watch(used, () => clear('flow_used'))
watch(resetQuota, () => clear('reset_quota_bytes'))
watch(resetPolicy, () => clear('reset_policy'))
watch(reason, () => clear('reason'))
const changes = computed(() => {
  const sub = props.subscription
  return {
    ...(editQuota.value && total.value !== sub.flow_total ? { flow_total: total.value } : {}),
    ...(editQuota.value && used.value !== sub.flow_used ? { flow_used: used.value } : {}),
    ...(editReset.value && resetQuota.value !== (sub.reset_quota_bytes || 0) ? { reset_quota_bytes: resetQuota.value } : {}),
    ...(editReset.value && scheduleChanged.value ? { reset_policy: resetPolicy.value } : {}),
  }
})
const hasChanges = computed(() => Object.keys(changes.value).length > 0)
const canSave = computed(() => hasChanges.value && !saving.value && utf8ByteLength(reason.value.trim()) >= 3 && utf8ByteLength(reason.value.trim()) <= 255
  && Object.values(changes.value).every(value => Number.isSafeInteger(value) && value >= 0) && (!editReset.value || policyOptions.value.some(option => option.value === resetPolicy.value && !option.disabled)))
async function confirmLeave() {
  if (saving.value) return false
  if (!hasChanges.value && !reason.value.trim()) return true
  return confirmAction({ title: '放弃未保存的修改？', message: '当前订阅设置尚未保存。', confirmText: '放弃修改', tone: 'danger' })
}
async function cancel() { if (await confirmLeave()) emit('cancel') }
defineExpose({ confirmLeave, dirty: computed(() => hasChanges.value || Boolean(reason.value.trim())), saving })
async function save() {
  if (saving.value || !hasChanges.value) return
  const validation: Record<string, string> = {}
  for (const [name, value] of Object.entries(changes.value)) {
    if (!Number.isSafeInteger(value) || value < 0) validation[name] = '请填写不小于零的有效流量。'
  }
  const length = utf8ByteLength(reason.value.trim())
  if (editReset.value && !policyOptions.value.some(option => option.value === resetPolicy.value && !option.disabled)) validation.reset_policy = '请选择适用于此订阅的重置策略。'
  if (length < 3 || length > 255) validation.reason = '请填写调整原因，长度为 3 到 255 字节。'
  if (!await applyValidation(validation, formElement)) return
  const sub = props.subscription
  const payload = { ...changes.value, reason: reason.value.trim() }
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
.reset-schedule { display: grid; align-content: start; gap: 8px; font-size: 13px; }
.reset-schedule > span { font-weight: 500; }
.reset-schedule > div,.quota-editor-footer { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
.quota-editor-footer > div { display: flex; gap: 8px; margin-left: auto; }
.reset-schedule small,.quota-editor-footer > span { color: var(--muted); font-size: 12px; }
</style>
