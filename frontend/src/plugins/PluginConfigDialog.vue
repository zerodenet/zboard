<template>
  <ModalDialog :open="true" title="高级配置" description="直接提交完整 JSON 配置，仅适用于熟悉该插件配置格式的管理员。" size="lg" :busy="saving" :dirty="dirty" @close="$emit('close')">
    <form id="plugin-json-form" ref="formElement" novalidate @submit.prevent="save">
      <PageAlert v-if="formErrors.formError.value" tone="danger">{{ formErrors.formError.value }}</PageAlert>
      <PageAlert v-if="error" tone="danger">{{ error }}<template #actions><UiButton variant="secondary" @click="load">重试</UiButton></template></PageAlert>
      <PageAlert v-if="dirty || saving" tone="warning" title="插件配置不能暂存到第三方登录页">完整 JSON 可能包含密钥，系统不会把它写入浏览器草稿。请留在本页使用邮箱密码重新认证，或自行安全另存、明确放弃修改后再打开第三方登录；当前内容不会自动保存或提交。</PageAlert>
      <PageAlert tone="warning">保存将完整替换旧配置，请包含需要保留的全部提供方和密钥。系统不会回显已有密钥。</PageAlert>
      <p v-if="loading" role="status">正在读取配置版本…</p>
      <p v-else-if="config">{{ config.configured ? '已有配置' : '尚未配置' }} · 修订 {{ config.revision }}</p>
      <FormField label="完整配置 JSON" name="plugin-json" required :error="formErrors.fields.config" v-slot="{ controlAttrs }">
        <UiTextarea v-bind="controlAttrs" v-model="text" rows="12" spellcheck="false" :disabled="saving || loading" class="plugin-json-input" />
      </FormField>
    </form>
    <template #footer="{ requestClose }">
      <UiButton variant="secondary" :disabled="saving" @click="requestClose">取消</UiButton>
      <UiButton form="plugin-json-form" type="submit" :disabled="saving || loading || !config || !!error" :loading="saving">保存完整配置</UiButton>
    </template>
  </ModalDialog>
</template>
<script setup lang="ts">
import { onScopeDispose, ref, watch } from 'vue'
import { onBeforeRouteUpdate } from 'vue-router'
import ModalDialog from '../components/ModalDialog.vue'
import PageAlert from '../components/PageAlert.vue'
import FormField from '../components/FormField.vue'
import UiTextarea from '../components/UiTextarea.vue'
import UiButton from '../components/UiButton.vue'
import { fetchPluginConfig, savePluginConfig, type ConfigView } from '../api/plugins'
import { useRemoteResource } from '../composables/useRemoteResource'
import { useDirtyForm, useFormErrors, useUnsavedChangesGuard } from '../composables/useFormState'
import { useReauthenticationDraft } from '../composables/useReauthenticationDraft'
const props = defineProps<{ pluginId: string }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const text = ref(''), saving = ref(false), formElement = ref<HTMLElement | null>(null)
const formErrors = useFormErrors()
const { dirty, markClean, confirmDiscard } = useDirtyForm(() => text.value)
// Register only a blocker. Never read, parse, sanitize, hash or persist JSON
// through the recovery system. Clean or closed dialogs contribute no draft.
useReauthenticationDraft({
  scope: 'admin.plugin-config.blocker', permission: 'admin', ready: () => false,
  read: () => dirty.value || saving.value ? { blocked: true } : null,
  sanitize: () => undefined,
  restore: () => false,
})
useUnsavedChangesGuard(() => saving.value || dirty.value, () => saving.value ? false : confirmDiscard())
onBeforeRouteUpdate(() => saving.value ? false : confirmDiscard())
let disposed = false
onScopeDispose(() => { disposed = true; text.value = '' })
const { data: config, loading, error, load, reset } = useRemoteResource<ConfigView | null>({
  initial: () => null, fetch: ({ signal }) => fetchPluginConfig(props.pluginId, signal), errorMessage: '无法读取配置版本，请重试。',
})
watch(text, () => formErrors.clear('config'))
watch(() => props.pluginId, () => { reset(); text.value = ''; markClean(); void load() }, { immediate: true })
async function save() {
  if (saving.value || loading.value || !config.value || error.value) return
  let value: unknown
  try { value = JSON.parse(text.value) } catch { value = null }
  if (!await formErrors.applyValidation(value && typeof value === 'object' && !Array.isArray(value) ? {} : { config: '请输入有效的 JSON 对象。' }, formElement)) return
  if (saving.value) return
  saving.value = true
  try {
    await savePluginConfig(props.pluginId, config.value.revision, value)
    if (!disposed) { text.value = ''; markClean(); emit('saved') }
  } catch (cause) { if (!disposed) await formErrors.applyApiError(cause, '保存失败，请检查配置；如版本已变更，请重新打开此弹窗。', formElement) }
  finally { saving.value = false }
}
</script>
