<template>
  <form ref="formElement" class="stack" aria-label="编辑订阅流量" novalidate @submit.prevent="save">
    <PageAlert v-if="formErrors.formError.value" tone="danger" title="流量修改失败">{{ formErrors.formError.value }}</PageAlert>
    <PageAlert v-if="success" tone="success" title="流量参数已保存">订阅授权已重新检查，节点同步已排队。</PageAlert>
    <FormField v-slot="{ controlAttrs }" label="当前周期配额" name="subscription-quota" :error="fields.flow_total" hint="包含本周期额外购买的流量，单位 GiB。">
      <ByteSizeInput v-model="total" v-bind="controlAttrs" :min-bytes="0" :max-bytes="Number.MAX_SAFE_INTEGER" :disabled="saving" />
    </FormField>
    <FormField v-slot="{ controlAttrs }" label="当前周期已用流量" name="subscription-used" :error="fields.flow_used" hint="填写需要设置的当前周期用量；未修改时保留最新上报用量。">
      <ByteSizeInput v-model="used" v-bind="controlAttrs" :min-bytes="0" :max-bytes="Number.MAX_SAFE_INTEGER" :disabled="saving" />
    </FormField>
    <FormField v-slot="{ controlAttrs }" label="每次重置配额" name="subscription-reset-quota" :error="fields.reset_quota_bytes" hint="自动重置和购买重置时恢复的额度，修改当前周期配额不会自动修改此值。">
      <ByteSizeInput v-model="resetQuota" v-bind="controlAttrs" :min-bytes="0" :max-bytes="Number.MAX_SAFE_INTEGER" :disabled="saving" />
    </FormField>
    <FormField v-slot="{ controlAttrs }" label="调整原因" name="subscription-quota-reason" :error="fields.reason" required>
      <UiTextarea v-model="reason" v-bind="controlAttrs" :disabled="saving" maxlength="255" rows="2" />
    </FormField>
    <p>剩余流量 {{ formatBytes(Math.max(0, total - used)) }}。套餐、到期时间和重置日期保持不变。</p>
    <UiButton type="submit" :disabled="!canSave" :loading="saving">保存流量参数</UiButton>
  </form>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { updateSubscriptionQuota, type AdminSubscriptionDetail } from '../api/client'
import ByteSizeInput from './ByteSizeInput.vue'
import FormField from './FormField.vue'
import PageAlert from './PageAlert.vue'
import UiButton from './UiButton.vue'
import UiTextarea from './UiTextarea.vue'
import { formatBytes } from '../utils/format'
import { useFormErrors } from '../composables/useFormState'
import { utf8ByteLength } from '../utils/validation'

const props = defineProps<{ subscription: AdminSubscriptionDetail }>()
const emit = defineEmits<{ saved: [] }>()
const total = ref(0), used = ref(0), resetQuota = ref(0), reason = ref('')
const saving = ref(false), success = ref(false)
const formElement = ref<HTMLElement | null>(null)
const formErrors = useFormErrors()
const { fields, clear, applyValidation, applyApiError } = formErrors
let attempt: { signature: string; id: string } | null = null
watch(() => props.subscription, sub => {
  total.value = sub.flow_total; used.value = sub.flow_used; resetQuota.value = sub.reset_quota_bytes || 0
  reason.value = ''; clear(); attempt = null
}, { immediate: true })
watch(total, () => clear('flow_total'))
watch(used, () => clear('flow_used'))
watch(resetQuota, () => clear('reset_quota_bytes'))
watch(reason, () => clear('reason'))
const changes = computed(() => {
  const sub = props.subscription
  return {
    ...(total.value !== sub.flow_total ? { flow_total: total.value } : {}),
    ...(used.value !== sub.flow_used ? { flow_used: used.value } : {}),
    ...(resetQuota.value !== (sub.reset_quota_bytes || 0) ? { reset_quota_bytes: resetQuota.value } : {}),
  }
})
const hasChanges = computed(() => Object.keys(changes.value).length > 0)
const canSave = computed(() => hasChanges.value && !saving.value && utf8ByteLength(reason.value.trim()) >= 3 && utf8ByteLength(reason.value.trim()) <= 255
  && [total.value, used.value, resetQuota.value].every(value => Number.isSafeInteger(value) && value >= 0))
async function save() {
  if (saving.value || !hasChanges.value) return
  const validation: Record<string, string> = {}
  for (const [name, value] of Object.entries({ flow_total: total.value, flow_used: used.value, reset_quota_bytes: resetQuota.value })) {
    if (!Number.isSafeInteger(value) || value < 0) validation[name] = '请填写不小于零的有效流量。'
  }
  const length = utf8ByteLength(reason.value.trim())
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
